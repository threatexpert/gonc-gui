package cn.threatexpert.gonc;

import org.junit.Test;

import java.io.File;
import java.nio.file.Files;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class ReceivedFileActionsTest {
    @Test
    public void recognizesApkAndCommonDocuments() {
        assertEquals("application/vnd.android.package-archive", ReceivedFileActions.fallbackMimeType("release.apk"));
        assertEquals("application/pdf", ReceivedFileActions.fallbackMimeType("manual.PDF"));
        assertEquals("application/octet-stream", ReceivedFileActions.fallbackMimeType("payload.unknownext"));
    }

    @Test
    public void viewAndChooserPlansGrantReadOnlyUriAccess() {
        ReceivedFileActions.IntentPlan direct = ReceivedFileActions.openPlan(
                "content://downloads/report", "application/pdf", false);
        assertEquals("android.intent.action.VIEW", direct.action);
        assertEquals("application/pdf", direct.mimeType);
        assertFalse(direct.chooser);
        assertTrue(direct.grantReadUriPermission);
        assertTrue(direct.attachClipData);
        assertFalse(direct.attachStream);

        ReceivedFileActions.IntentPlan chooser = ReceivedFileActions.openPlan(
                "content://downloads/app", ReceivedFileActions.fallbackMimeType("app.apk"), true);
        assertTrue(chooser.chooser);
        assertEquals("application/vnd.android.package-archive", chooser.mimeType);
        assertTrue(chooser.grantReadUriPermission);
        assertTrue(chooser.attachClipData);
    }

    @Test
    public void sharePlanUsesSendStreamChooserAndReadOnlyGrant() {
        ReceivedFileActions.IntentPlan share = ReceivedFileActions.sharePlan(
                "content://downloads/photo", "image/jpeg");
        assertEquals("android.intent.action.SEND", share.action);
        assertTrue(share.chooser);
        assertTrue(share.attachStream);
        assertTrue(share.attachClipData);
        assertTrue(share.grantReadUriPermission);
    }

    @Test
    public void legacyFileMustRemainInsideCanonicalDownloadsRoot() throws Exception {
        File root = Files.createTempDirectory("gonc-root").toFile();
        File child = new File(root, "nested/report.txt");
        File sibling = Files.createTempDirectory("gonc-sibling").toFile();

        assertTrue(ReceivedFileActions.isCanonicalChild(root, child));
        assertFalse(ReceivedFileActions.isCanonicalChild(root, root));
        assertFalse(ReceivedFileActions.isCanonicalChild(root, new File(sibling, "report.txt")));
        assertFalse(ReceivedFileActions.isCanonicalChild(root, new File(root, "../escape.txt")));
    }
}
