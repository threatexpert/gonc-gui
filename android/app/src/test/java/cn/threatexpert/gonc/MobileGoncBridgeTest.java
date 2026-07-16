package cn.threatexpert.gonc;

import static org.junit.Assert.assertEquals;

import org.junit.Test;

import java.util.concurrent.atomic.AtomicInteger;

public final class MobileGoncBridgeTest {
    @Test
    public void stopReleasesMulticastLeaseOnce() {
        MobileGoncBridge.BridgeSession session = new MobileGoncBridge.BridgeSession();
        AtomicInteger releases = new AtomicInteger();

        session.attachMulticastLease(releases::incrementAndGet);
        session.stop();
        session.stop();

        assertEquals(1, releases.get());
    }

    @Test
    public void leaseAttachedAfterStopIsReleasedImmediately() {
        MobileGoncBridge.BridgeSession session = new MobileGoncBridge.BridgeSession();
        AtomicInteger releases = new AtomicInteger();

        session.stop();
        session.attachMulticastLease(releases::incrementAndGet);

        assertEquals(1, releases.get());
    }

    @Test
    public void naturalFinishReleasesMulticastLeaseOnce() {
        MobileGoncBridge.BridgeSession session = new MobileGoncBridge.BridgeSession();
        AtomicInteger releases = new AtomicInteger();

        session.attachMulticastLease(releases::incrementAndGet);
        session.finish();
        session.finish();

        assertEquals(1, releases.get());
    }
}
