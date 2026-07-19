package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;

public class ReceivedFileActionsTest {
    @Test
    public void recognizesApkAndCommonDocuments() {
        assertEquals("application/vnd.android.package-archive", ReceivedFileActions.fallbackMimeType("release.apk"));
        assertEquals("application/pdf", ReceivedFileActions.fallbackMimeType("manual.PDF"));
        assertEquals("application/octet-stream", ReceivedFileActions.fallbackMimeType("payload.unknownext"));
    }
}
