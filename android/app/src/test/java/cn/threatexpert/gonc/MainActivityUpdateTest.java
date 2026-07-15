package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;

public class MainActivityUpdateTest {
    @Test
    public void failureKindsMapToLocalizedMessages() {
        assertEquals(R.string.update_network_error,
                MainActivity.updateFailureMessage(AndroidUpdateChecker.FailureKind.NETWORK));
        assertEquals(R.string.update_manifest_error,
                MainActivity.updateFailureMessage(AndroidUpdateChecker.FailureKind.INVALID_MANIFEST));
        assertEquals(R.string.update_platform_error,
                MainActivity.updateFailureMessage(AndroidUpdateChecker.FailureKind.UNSUPPORTED_PLATFORM));
    }
}
