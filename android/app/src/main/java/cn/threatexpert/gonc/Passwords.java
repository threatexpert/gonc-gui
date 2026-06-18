package cn.threatexpert.gonc;

import java.security.SecureRandom;
import java.util.Locale;

final class Passwords {
    private static final char[] CHARS = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789".toCharArray();
    private static final SecureRandom RANDOM = new SecureRandom();

    private Passwords() {
    }

    static String generate() {
        while (true) {
            String value = generateRandomString(24);
            if (!isWeak(value)) {
                return value;
            }
        }
    }

    static boolean isWeak(String password) {
        if (password == null || password.length() < 8) {
            return true;
        }

        String lowerPassword = password.toLowerCase(Locale.ROOT);
        String[] weakList = {
                "123456", "password", "12345678", "qwerty", "abc123", "111111", "123123"
        };
        for (String weak : weakList) {
            if (lowerPassword.equals(weak)) {
                return true;
            }
        }

        boolean hasLetter = false;
        boolean hasDigit = false;
        for (int i = 0; i < password.length(); i++) {
            char c = password.charAt(i);
            if (Character.isLetter(c)) {
                hasLetter = true;
            }
            if (Character.isDigit(c)) {
                hasDigit = true;
            }
        }

        return !hasLetter || !hasDigit;
    }

    private static String generateRandomString(int length) {
        char[] value = new char[length];
        for (int i = 0; i < value.length; i++) {
            value[i] = CHARS[RANDOM.nextInt(CHARS.length)];
        }
        return new String(value);
    }
}
