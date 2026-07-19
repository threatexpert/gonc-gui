package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertTrue;

public class TransferInlineQrStateTest {
    @Test
    public void sendMaskLatchesAfterFirstConnection() {
        assertFalse(TransferInlineQrState.latchSendConnected(false, 0));
        assertTrue(TransferInlineQrState.latchSendConnected(false, 1));
        assertTrue(TransferInlineQrState.latchSendConnected(true, 0));
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
    public void cacheKeyUsesTrimmedPassphraseAndSize() {
        assertEquals(new TransferInlineQrState.CacheKey(" secret ", 112),
                new TransferInlineQrState.CacheKey("secret", 112));
        assertNotEquals(new TransferInlineQrState.CacheKey("secret", 112),
                new TransferInlineQrState.CacheKey("secret", 220));
    }
}
