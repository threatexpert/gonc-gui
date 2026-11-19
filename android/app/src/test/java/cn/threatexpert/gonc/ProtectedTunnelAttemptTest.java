package cn.threatexpert.gonc;

import org.junit.Test;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import static org.junit.Assert.*;

public class ProtectedTunnelAttemptTest {
    @Test public void competingReportsDoNotDisconnectWinningTunnel() {
        ProtectedTunnelAttempt attempt = new ProtectedTunnelAttempt();
        assertTrue(attempt.report("winner", "connected"));
        assertTrue(attempt.ready("socks5://127.0.0.1:1000"));
        assertFalse(attempt.report("loser", "error: timeout"));
        assertFalse(attempt.report("winner", "negotiating"));
        assertFalse(attempt.lost());
        assertTrue(attempt.report("winner", "disconnected"));
        assertTrue(attempt.lost());
        assertFalse(attempt.ready("socks5://127.0.0.1:9999"));
        assertFalse(attempt.report("winner", "connected"));
        assertEquals("", attempt.endpoint());
    }

    @Test public void liveAttemptSleepsUntilLossAndRetiredCallbackCannotReviveIt() throws Exception {
        ProtectedTunnelAttempt attempt = new ProtectedTunnelAttempt();
        attempt.report("a", "connected");
        attempt.ready("socks5://127.0.0.1:1000");
        CountDownLatch waiting = new CountDownLatch(1), returned = new CountDownLatch(1);
        Thread worker = new Thread(() -> {
            waiting.countDown();
            try { attempt.awaitChange(1); } catch (InterruptedException e) { throw new AssertionError(e); }
            returned.countDown();
        });
        worker.start();
        try {
            assertTrue(waiting.await(1, TimeUnit.SECONDS));
            assertFalse(returned.await(50, TimeUnit.MILLISECONDS));
            attempt.fail();
            assertTrue(returned.await(1, TimeUnit.SECONDS));
            attempt.retire();
            assertFalse(attempt.report("b", "connected"));
            assertFalse(attempt.ready("socks5://127.0.0.1:2000"));
        } finally { attempt.retire(); worker.join(1000); }
    }

    @Test public void stopWakesConnectingAttemptWithoutNetworkPolling() throws Exception {
        ProtectedTunnelAttempt attempt = new ProtectedTunnelAttempt();
        CountDownLatch returned = new CountDownLatch(1);
        Thread worker = new Thread(() -> {
            try { attempt.awaitChange(0); } catch (InterruptedException e) { throw new AssertionError(e); }
            returned.countDown();
        });
        worker.start();
        attempt.retire();
        assertTrue(returned.await(1, TimeUnit.SECONDS));
        worker.join(1000);
        assertFalse(attempt.ready("socks5://127.0.0.1:1000"));
    }
}
