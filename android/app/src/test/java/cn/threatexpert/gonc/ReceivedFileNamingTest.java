package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class ReceivedFileNamingTest {
    @Test
    public void numberedNamesPreserveFileExtensions() {
        assertEquals("a.apk", HttpReceiver.numberedFileName("a.apk", 0));
        assertEquals("a (1).apk", HttpReceiver.numberedFileName("a.apk", 1));
        assertEquals("a (2).APK", HttpReceiver.numberedFileName("a.APK", 2));
        assertEquals("archive.tar (3).gz", HttpReceiver.numberedFileName("archive.tar.gz", 3));
        assertEquals("README (1)", HttpReceiver.numberedFileName("README", 1));
        assertEquals(".nomedia (1)", HttpReceiver.numberedFileName(".nomedia", 1));
    }

    @Test
    public void usesApkMimeForSavingAndOpening() {
        String apkMime = "application/vnd.android.package-archive";
        String name = HttpReceiver.numberedFileName("a.APK", 1);
        assertEquals(apkMime, HttpReceiver.mimeTypeForSave(name, extension -> "application/octet-stream"));
        assertEquals(apkMime, ReceivedFileActions.fallbackMimeType(name));
        assertEquals("application/pdf", HttpReceiver.mimeTypeForSave(
                "manual.PDF", extension -> "pdf".equals(extension) ? "application/pdf" : null));
        assertEquals("application/octet-stream", HttpReceiver.mimeTypeForSave("file.unknown", extension -> null));
    }

    @Test
    public void verifiesActualNameAfterProviderResolvesHiddenCollisions() {
        assertTrue(HttpReceiver.acceptsCreatedName("a.apk", "a.apk", "a.apk"));
        assertTrue(HttpReceiver.acceptsCreatedName("a.apk", "a.apk", "a (1).apk"));
        assertTrue(HttpReceiver.acceptsCreatedName("a.apk", "a (2).apk", "a (2).apk"));
        assertFalse(HttpReceiver.acceptsCreatedName("a.apk", "a.apk", "a.apk(1)"));
        assertFalse(HttpReceiver.acceptsCreatedName("a.apk", "a.apk", "a(1).apk"));
        assertFalse(HttpReceiver.acceptsCreatedName("a.apk", "a.apk", "a (1) (1).apk"));
        assertFalse(HttpReceiver.acceptsCreatedName("a.apk", "a (1).apk", "a (1) (1).apk"));
        assertFalse(HttpReceiver.acceptsCreatedName("a.apk", "a.apk", null));
        assertTrue(HttpReceiver.acceptsCreatedName("question?.apk", "question?.apk", "question_.apk"));
    }
}
