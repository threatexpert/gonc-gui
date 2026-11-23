package cn.threatexpert.gonc;

import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;

import java.security.KeyStore;
import java.security.SecureRandom;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * The 32-byte key that seals the Nearby store (identity and pairing keys).
 * The key itself is kept in preferences only in wrapped form, encrypted by an
 * AES key that lives in Android Keystore and never leaves the device, so a
 * copy of the app's files or preferences does not reveal pairing keys.
 */
final class NearbyKey {
    private static final String KEYSTORE = "AndroidKeyStore";
    private static final String ALIAS = "gonc-nearby-store";
    private static final String PREF = "nearby_store_key";
    private static final int KEY_BYTES = 32;
    private static final int TAG_BITS = 128;

    private NearbyKey() {
    }

    /** Result of {@link #load}: the key, and whether it was newly created. */
    static final class Loaded {
        final byte[] key;
        final boolean created;

        Loaded(byte[] key, boolean created) {
            this.key = key;
            this.created = created;
        }
    }

    /**
     * Returns the store key, creating one on first use. When a wrapped key
     * exists but can no longer be unwrapped (for example after the app's
     * preferences were restored onto another device, where the Keystore key
     * does not exist), a new key is created and {@code created} is true: the
     * old store cannot be opened anyway and must be replaced.
     */
    static Loaded load(SharedPreferences prefs) throws Exception {
        SecretKey wrapping = wrappingKey();
        String stored = prefs.getString(PREF, "");
        if (!stored.isEmpty()) {
            try {
                return new Loaded(unwrap(wrapping, stored), false);
            } catch (Exception ignored) {
                // Fall through and replace the unusable key.
            }
        }
        byte[] key = new byte[KEY_BYTES];
        new SecureRandom().nextBytes(key);
        prefs.edit().putString(PREF, wrap(wrapping, key)).apply();
        return new Loaded(key, true);
    }

    private static SecretKey wrappingKey() throws Exception {
        KeyStore store = KeyStore.getInstance(KEYSTORE);
        store.load(null);
        KeyStore.Entry entry = store.getEntry(ALIAS, null);
        if (entry instanceof KeyStore.SecretKeyEntry) {
            return ((KeyStore.SecretKeyEntry) entry).getSecretKey();
        }
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE);
        generator.init(new KeyGenParameterSpec.Builder(ALIAS,
                KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build());
        return generator.generateKey();
    }

    private static String wrap(SecretKey wrapping, byte[] key) throws Exception {
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.ENCRYPT_MODE, wrapping);
        byte[] iv = cipher.getIV();
        byte[] sealed = cipher.doFinal(key);
        return Base64.encodeToString(iv, Base64.NO_WRAP) + ":" + Base64.encodeToString(sealed, Base64.NO_WRAP);
    }

    private static byte[] unwrap(SecretKey wrapping, String stored) throws Exception {
        int split = stored.indexOf(':');
        if (split <= 0) {
            throw new IllegalArgumentException("bad wrapped key");
        }
        byte[] iv = Base64.decode(stored.substring(0, split), Base64.NO_WRAP);
        byte[] sealed = Base64.decode(stored.substring(split + 1), Base64.NO_WRAP);
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.DECRYPT_MODE, wrapping, new GCMParameterSpec(TAG_BITS, iv));
        byte[] key = cipher.doFinal(sealed);
        if (key.length != KEY_BYTES) {
            throw new IllegalArgumentException("bad key length");
        }
        return key;
    }
}
