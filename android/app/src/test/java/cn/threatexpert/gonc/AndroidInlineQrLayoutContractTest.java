package cn.threatexpert.gonc;

import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class AndroidInlineQrLayoutContractTest {
    private static String source(String fileName) throws Exception {
        return new String(Files.readAllBytes(Paths.get(
                "src/main/java/cn/threatexpert/gonc/" + fileName)),
                StandardCharsets.UTF_8);
    }

    @Test
    public void sharedInlineQrHasNoPrivateFrame() throws Exception {
        String source = source("PassphraseQrView.java");
        assertFalse(source.contains("FrameLayout"));
        assertFalse(source.contains("container.setPadding"));
        assertFalse(source.contains("container.setBackground"));
        assertTrue(source.contains("return image;"));
    }

    @Test
    public void sendInlineQrUsesExplicitSquareSizeAndCenteredChildGravity() throws Exception {
        String source = source("SendController.java");
        int start = source.indexOf("private View passwordField()");
        int end = source.indexOf("private View protocolToggle()", start);
        assertTrue(start >= 0 && end > start);
        String method = source.substring(start, end);
        assertTrue(method.contains(
                "int qrSize = u.dp(TransferInlineQrState.inlineQrSizeDp());"));
        assertTrue(method.contains(
                "new LinearLayout.LayoutParams(qrSize, qrSize)"));
        assertTrue(method.contains("qrParams.gravity = Gravity.CENTER_HORIZONTAL;"));
        assertTrue(method.contains("qrOnly.addView(qr, qrParams);"));
    }

    @Test
    public void receiveInlineQrUsesExplicitSquareSize() throws Exception {
        String source = source("ReceiveController.java");
        int start = source.indexOf("private View receiveSessionBarView()");
        int end = source.indexOf("private View receiveProgressContent()", start);
        assertTrue(start >= 0 && end > start);
        String method = source.substring(start, end);
        assertTrue(method.contains(
                "int qrSize = dp(TransferInlineQrState.inlineQrSizeDp());"));
        assertTrue(method.contains(
                "row.addView(qr, new LinearLayout.LayoutParams(qrSize, qrSize));"));
        assertTrue(method.contains("row.setBackground(rounded("));
    }
}
