package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertTrue;

public class TransferInlineQrStateTest {
    @Test
    public void sendMaskLatchesAfterFirstConnection() {
        boolean latched = false;
        latched = TransferInlineQrState.latchSendConnected(latched, 0);
        assertFalse(latched);
        latched = TransferInlineQrState.latchSendConnected(latched, 1);
        assertTrue(latched);
        latched = TransferInlineQrState.latchSendConnected(latched, 0);
        assertTrue(latched);
        assertFalse(TransferInlineQrState.newSendRunLatch());
    }

    @Test
    public void receiveQrRetiresAfterSuccessOrAbnormalState() {
        assertTrue(TransferInlineQrState.showReceiveQr(false, "starting"));
        assertTrue(TransferInlineQrState.showReceiveQr(false, "connecting"));
        assertFalse(TransferInlineQrState.showReceiveQr(false, "reconnecting"));
        assertTrue(TransferInlineQrState.retireReceiveQr(false, "connected"));
        assertTrue(TransferInlineQrState.retireReceiveQr(false, "error"));
        assertFalse(TransferInlineQrState.showReceiveQr(true, "waiting"));
    }

    @Test
    public void receiveQrRetirementPersistsForWholeRun() {
        boolean retired = TransferInlineQrState.newReceiveRunRetired();
        assertFalse(retired);
        assertTrue(TransferInlineQrState.showReceiveQr(retired, "starting"));
        retired = TransferInlineQrState.retireReceiveQr(retired, "error: timeout");
        assertTrue(retired);
        assertFalse(TransferInlineQrState.showReceiveQr(retired, "waiting"));
        assertFalse(TransferInlineQrState.showReceiveQr(retired, "reconnecting"));

        assertTrue(TransferInlineQrState.retireReceiveQr(false, "failed handshake"));
        assertTrue(TransferInlineQrState.retireReceiveQr(false, "lost peer"));
        assertTrue(TransferInlineQrState.retireReceiveQr(false, "timeout waiting"));
    }

    @Test
    public void cacheKeyUsesTrimmedPassphraseAndSize() {
        assertEquals(new TransferInlineQrState.CacheKey(" secret ", 112),
                new TransferInlineQrState.CacheKey("secret", 112));
        assertNotEquals(new TransferInlineQrState.CacheKey("secret", 112),
                new TransferInlineQrState.CacheKey("secret", 220));
    }
}
