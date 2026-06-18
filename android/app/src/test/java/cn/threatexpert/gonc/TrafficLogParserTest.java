package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

public class TrafficLogParserTest {
    @Test
    public void parsesSingleConnectionTrafficLine() {
        TrafficLogParser.Traffic traffic = TrafficLogParser.parse("IN: 12.0 KiB (12288 bytes), 1.0 KiB/s | OUT: 2.0 MiB (2097152 bytes), 64.0 KiB/s | 00:00:12");

        assertNotNull(traffic);
        assertEquals(1024.0, traffic.inBps, 0.1);
        assertEquals(64.0 * 1024, traffic.outBps, 0.1);
        assertTrue(TrafficLogParser.isProgressLog("IN: 12.0 KiB (12288 bytes), 1.0 KiB/s | OUT: 2.0 MiB (2097152 bytes), 64.0 KiB/s | 00:00:12"));
    }

    @Test
    public void parsesMultiConnectionTrafficLine() {
        String line = "IN: 74.1 KiB (75891 bytes), 0.0 B/s | OUT: 145.9 MiB (152976138 bytes), 64.0 KiB/s | 2 | 00:07:27";

        TrafficLogParser.Traffic traffic = TrafficLogParser.parse(line);

        assertNotNull(traffic);
        assertEquals(0.0, traffic.inBps, 0.1);
        assertEquals(64.0 * 1024, traffic.outBps, 0.1);
        assertTrue(TrafficLogParser.isProgressLog(line));
    }

    @Test
    public void ignoresHttpServerBusinessLogs() {
        String line = "20260617-233203 [HTTPSRV] Serving file '/tlsfw/cert.pem' (size 1.5 KB) to 120.230.80.107:22497";

        assertNull(TrafficLogParser.parse(line));
        assertFalse(TrafficLogParser.isProgressLog(line));
    }
}
