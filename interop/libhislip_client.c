/*
 * Interoperability test for the gotmc/hislip server, built against
 * lxi-tools/libhislip.
 *
 * libhislip is an independent C implementation. It is the only one of the three
 * clients used here that exercises the maximum message size transaction from a
 * non-Python stack, and finding two bugs in that transaction was its entire
 * contribution; see interop/README.md and §24.4.2 of the design specification.
 *
 * Two limitations of the library constrain what this file can test, and neither
 * is a defect in the server:
 *
 *   hs_sync_send computes a chunk count but then passes the full length and an
 *   unadvanced data pointer to msg_create, so it emits one message however large
 *   and violates the maximum message size it has just negotiated. Messages here
 *   are therefore kept inside that maximum.
 *
 *   hs_sync_receive loops until DataEnd but its payload reassembly is two TODO
 *   comments with no code, so it cannot receive a chunked response. A correctly
 *   chunking server appears to hang to it. Responses here are kept to one packet.
 *
 * Its client API has no device clear, trigger or lock, so those need a different
 * client.
 *
 * Build:
 *   gcc -o libhislip_client libhislip_client.c \
 *       -I<libhislip>/src -L<libhislip>/build/src -lhislip
 */

#include <hislip/client.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures = 0;
static int passes = 0;

static void report(const char *name, int ok, const char *detail)
{
    if (ok) {
        passes++;
        printf("  PASS %s: %s\n", name, detail);
    } else {
        failures++;
        printf("  FAIL %s: %s\n", name, detail);
    }
    fflush(stdout);
}

int main(int argc, char **argv)
{
    const char *host = (argc > 1) ? argv[1] : "127.0.0.1";
    int port = (argc > 2) ? atoi(argv[2]) : HISLIP_PORT;

    printf("libhislip -> %s:%d\n", host, port);
    fflush(stdout);

    hs_device_t dev = hs_connect((char *)host, port, "hislip0", 2000);
    if (dev < 0) {
        printf("  FAIL connect: hs_connect returned %d\n", dev);
        return 1;
    }
    report("connect", 1, "both channels initialized");

    /*
     * The maximum message size transaction. The reply must carry the server's own
     * receive limit, not the smaller of the two values: IVI-6.1 Table 28 makes
     * the two directions independent. An implementation that returns a negotiated
     * minimum and applies it to its transmit limit gets both halves wrong, which
     * is what this call found.
     */
    uint64_t reported = hs_set_maximum_message_size(dev, 4096, 2000);
    char detail[256];
    snprintf(detail, sizeof detail, "server reported its receive limit as %llu",
             (unsigned long long)reported);
    report("maximum message size", reported > 0, detail);

    char buf[8192];
    uint64_t n;

    snprintf(buf, sizeof buf, "*IDN?\n");
    hs_sync_send(dev, buf, strlen(buf), 2000);
    memset(buf, 0, sizeof buf);
    n = hs_sync_receive(dev, buf, sizeof buf - 1, 2000);
    report("*IDN? query", n > 0 && strstr(buf, "GoTMC") != NULL, buf);

    /*
     * A command must produce no response. Querying afterwards and getting only
     * that query's answer is what proves nothing was left queued.
     */
    snprintf(buf, sizeof buf, "SOUR:VOLT 2.5\n");
    hs_sync_send(dev, buf, strlen(buf), 2000);
    snprintf(buf, sizeof buf, "MEAS:VOLT?\n");
    hs_sync_send(dev, buf, strlen(buf), 2000);
    memset(buf, 0, sizeof buf);
    n = hs_sync_receive(dev, buf, sizeof buf - 1, 2000);
    report("command then query", n > 0 && strstr(buf, "2.5") != NULL, buf);

    /*
     * A compound message whose only query marker falls in its tail. An
     * accumulate-then-classify response policy truncates and answers this
     * wrongly; see §24.4.2. 500 repetitions keeps it inside the server's
     * 8192-byte maximum, which libhislip would otherwise exceed.
     */
    char *big = malloc(16384);
    if (big == NULL) {
        printf("  FAIL allocation\n");
        return 1;
    }
    big[0] = '\0';
    for (int i = 0; i < 500; i++) {
        strcat(big, "SOUR:VOLT 3.3;");
    }
    strcat(big, "MEAS:VOLT?\n");
    size_t biglen = strlen(big);
    hs_sync_send(dev, big, biglen, 5000);
    free(big);
    memset(buf, 0, sizeof buf);
    n = hs_sync_receive(dev, buf, sizeof buf - 1, 5000);
    snprintf(detail, sizeof detail, "%zu byte compound -> %.32s", biglen, buf);
    report("long compound write then read",
           n > 0 && strstr(buf, "3.3") != NULL, detail);

    /* A question mark inside a quoted string is data, not a query marker. */
    snprintf(buf, sizeof buf, "DISP:TEXT \"what?\"\n");
    hs_sync_send(dev, buf, strlen(buf), 2000);
    snprintf(buf, sizeof buf, "*IDN?\n");
    hs_sync_send(dev, buf, strlen(buf), 2000);
    memset(buf, 0, sizeof buf);
    n = hs_sync_receive(dev, buf, sizeof buf - 1, 2000);
    report("question mark inside a string",
           n > 0 && strstr(buf, "GoTMC") != NULL && strstr(buf, "what") == NULL,
           buf);

    hs_disconnect(dev);
    report("disconnect", 1, "session closed");

    printf("\nlibhislip: %d passed, %d failed\n", passes, failures);
    return failures == 0 ? 0 : 1;
}
