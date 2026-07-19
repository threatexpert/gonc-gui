package cn.threatexpert.gonc;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Locale;
import java.util.Objects;
import java.util.Set;

final class TransferInlineQrState {
    private static final int QR_BITMAP_CACHE_BUDGET_BYTES = 4 * 1024 * 1024;
    private static final int MAX_QR_PIXEL_SIZE = 512;
    private static final Set<String> INITIAL_RECEIVE_STATES =
            new HashSet<>(Arrays.asList(
                    "wait", "waiting", "starting", "preparing", "connecting", "negotiating"));

    private TransferInlineQrState() {
    }

    static boolean latchSendConnected(boolean latched, int connectedCount) {
        return latched || connectedCount > 0;
    }

    static boolean newSendRunLatch() {
        return false;
    }

    static boolean newReceiveRunRetired() {
        return false;
    }

    static int qrBitmapCacheBudgetBytes() {
        return QR_BITMAP_CACHE_BUDGET_BYTES;
    }

    static int capQrPixelSize(int requestedPixelSize) {
        return Math.max(1, Math.min(MAX_QR_PIXEL_SIZE, requestedPixelSize));
    }

    static BitmapCacheKey productionCacheKey(String passphrase, int requestedPixelSize, boolean masked) {
        String clean = passphrase == null ? "" : passphrase.trim();
        return new BitmapCacheKey(
                sha256(clean), capQrPixelSize(requestedPixelSize), masked ? "masked" : "clear");
    }

    static boolean shouldClearQrCache(long currentRunId, long eventRunId) {
        return currentRunId > 0 && currentRunId == eventRunId;
    }

    static boolean showReceiveQr(boolean retired, String state) {
        return !retired && INITIAL_RECEIVE_STATES.contains(normalize(state));
    }

    static boolean retireReceiveQr(boolean retired, String state) {
        if (retired) {
            return true;
        }
        String clean = normalize(state);
        if ("error".equals(clean) || clean.startsWith("error:")
                || clean.startsWith("fail")
                || clean.startsWith("lost")
                || clean.startsWith("timeout")) {
            return true;
        }
        return !INITIAL_RECEIVE_STATES.contains(clean);
    }

    private static String normalize(String value) {
        return value == null ? "" : value.trim().toLowerCase(Locale.ROOT);
    }

    private static String sha256(String value) {
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256")
                    .digest(value.getBytes(StandardCharsets.UTF_8));
            StringBuilder encoded = new StringBuilder(digest.length * 2);
            for (byte item : digest) {
                encoded.append(String.format(Locale.ROOT, "%02x", item & 0xff));
            }
            return encoded.toString();
        } catch (NoSuchAlgorithmException error) {
            throw new IllegalStateException("SHA-256 unavailable", error);
        }
    }

    static final class BitmapCacheKey {
        private final String digest;
        private final int pixelSize;
        private final String variant;

        BitmapCacheKey(String digest, int pixelSize, String variant) {
            this.digest = digest;
            this.pixelSize = pixelSize;
            this.variant = variant;
        }

        int pixelSize() {
            return pixelSize;
        }

        @Override
        public boolean equals(Object other) {
            if (this == other) {
                return true;
            }
            if (!(other instanceof BitmapCacheKey)) {
                return false;
            }
            BitmapCacheKey that = (BitmapCacheKey) other;
            return pixelSize == that.pixelSize
                    && digest.equals(that.digest)
                    && variant.equals(that.variant);
        }

        @Override
        public int hashCode() {
            return Objects.hash(digest, pixelSize, variant);
        }

        @Override
        public String toString() {
            return digest + ":" + pixelSize + ":" + variant;
        }
    }

    static final class CacheKey {
        private final String passphrase;
        private final int pixelSize;

        CacheKey(String passphrase, int pixelSize) {
            this.passphrase = passphrase == null ? "" : passphrase.trim();
            this.pixelSize = pixelSize;
        }

        @Override
        public boolean equals(Object other) {
            if (this == other) {
                return true;
            }
            if (!(other instanceof CacheKey)) {
                return false;
            }
            CacheKey that = (CacheKey) other;
            return pixelSize == that.pixelSize && passphrase.equals(that.passphrase);
        }

        @Override
        public int hashCode() {
            return Objects.hash(passphrase, pixelSize);
        }
    }
}
