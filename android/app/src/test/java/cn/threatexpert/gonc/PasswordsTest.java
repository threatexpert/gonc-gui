package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class PasswordsTest {
    @Test
    public void weakPasswordRulesMatchGoncCore() {
        assertTrue(Passwords.isWeak("123456"));
        assertTrue(Passwords.isWeak("password"));
        assertTrue(Passwords.isWeak("abcdefgh"));
        assertTrue(Passwords.isWeak("12345678"));
        assertFalse(Passwords.isWeak("abc12345"));
    }

    @Test
    public void generatedPasswordIsNotWeak() {
        for (int i = 0; i < 200; i++) {
            assertFalse(Passwords.isWeak(Passwords.generate()));
        }
    }
}
