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

    @Test
    public void productionCacheCapsPixelsAndUsesStrictByteBudget() {
        assertEquals(4 * 1024 * 1024, TransferInlineQrState.qrBitmapCacheBudgetBytes());
        assertEquals(320, TransferInlineQrState.capQrPixelSize(320));
        assertEquals(512, TransferInlineQrState.capQrPixelSize(880));
    }

    @Test
    public void productionCacheKeyUsesDigestSizeAndVariantWithoutRawPassphrase() {
        TransferInlineQrState.BitmapCacheKey clear =
                TransferInlineQrState.productionCacheKey(" secret ", 880, false);
        TransferInlineQrState.BitmapCacheKey same =
                TransferInlineQrState.productionCacheKey("secret", 512, false);
        TransferInlineQrState.BitmapCacheKey masked =
                TransferInlineQrState.productionCacheKey("secret", 512, true);

        assertEquals(clear, same);
        assertNotEquals(clear, masked);
        assertEquals(512, clear.pixelSize());
        assertFalse(clear.toString().contains("secret"));
    }

    @Test
    public void staleRunCannotClearCurrentQrCache() {
        assertTrue(TransferInlineQrState.shouldClearQrCache(9, 9));
        assertFalse(TransferInlineQrState.shouldClearQrCache(9, 8));
        assertFalse(TransferInlineQrState.shouldClearQrCache(0, 0));
    }
}
