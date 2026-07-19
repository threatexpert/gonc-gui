package cn.threatexpert.gonc;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Locale;
import java.util.Objects;
import java.util.Set;

final class TransferInlineQrState {
    private static final Set<String> INITIAL_RECEIVE_STATES =
            new HashSet<>(Arrays.asList(
                    "wait", "waiting", "starting", "preparing", "connecting", "negotiating"));

    private TransferInlineQrState() {
    }

    static boolean latchSendConnected(boolean latched, int connectedCount) {
        return latched || connectedCount > 0;
    }

    static boolean showReceiveQr(boolean retired, String state) {
        return !retired && INITIAL_RECEIVE_STATES.contains(normalize(state));
    }

    static boolean retireReceiveQr(boolean retired, String state) {
        return retired || !INITIAL_RECEIVE_STATES.contains(normalize(state));
    }

    private static String normalize(String value) {
        return value == null ? "" : value.trim().toLowerCase(Locale.ROOT);
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
