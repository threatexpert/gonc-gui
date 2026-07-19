package cn.threatexpert.gonc;

import org.junit.Test;

import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.Assert.assertEquals;

public class HttpReceiverSessionTest {
    @Test
    public void stopDoesNotReportTerminationUntilWorkerActuallyEnds() {
        HttpReceiver.Session session = new HttpReceiver.Session();
        AtomicInteger terminations = new AtomicInteger();
        session.onTerminated(terminations::incrementAndGet);

        session.stop();
        assertEquals(0, terminations.get());

        session.markTerminated();
        session.markTerminated();
        assertEquals(1, terminations.get());
    }

    @Test
    public void listenerRegisteredAfterTerminationRunsExactlyOnce() {
        HttpReceiver.Session session = new HttpReceiver.Session();
        session.markTerminated();
        AtomicInteger terminations = new AtomicInteger();

        session.onTerminated(terminations::incrementAndGet);
        assertEquals(1, terminations.get());
    }
}
