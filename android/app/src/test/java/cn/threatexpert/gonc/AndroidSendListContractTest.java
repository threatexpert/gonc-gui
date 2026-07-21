package cn.threatexpert.gonc;

import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class AndroidSendListContractTest {
    @Test
    public void listUsesApprovedEmptyAndPopulatedAddSurfaces() throws Exception {
        String send = source("SendController.java");
        String chinese = resource("values-zh/strings.xml");

        assertFalse(send.contains("Button add = u.secondaryButton"));
        assertFalse(send.contains("Button addFolder = u.secondaryButton"));
        assertTrue(chinese.contains("点击这里添加文件、文件夹、媒体或文字"));
        assertTrue(chinese.contains("点击这里继续添加"));
        assertTrue(send.contains("showAddTypeDialog"));
        assertTrue(send.contains("R.string.send_empty_add_hint"));
        assertTrue(send.contains("R.string.send_continue_add"));
    }

    @Test
    public void populatedListHasClearAndCompactCircularRemove() throws Exception {
        String send = source("SendController.java");

        assertTrue(send.contains("clearItems"));
        assertTrue(send.contains("R.string.send_content_title_count"));
        assertTrue(send.contains("remove.setText(\"×\")"));
        assertTrue(send.contains("u.dp(32)"));
        assertTrue(send.contains("remove.setContentDescription"));
    }

    @Test
    public void pickerChoicesUseConfirmedOrderAndIcons() throws Exception {
        String send = source("SendController.java");
        String chinese = resource("values-zh/strings.xml");
        int start = send.indexOf("private void showAddTypeDialog()");
        int end = send.indexOf("private void showAuthoredTextDialog()", start);
        assertTrue(start >= 0 && end > start);
        String dialog = send.substring(start, end);

        int file = dialog.indexOf("R.string.add_files");
        int folder = dialog.indexOf("R.string.add_folder", file);
        int media = dialog.indexOf("R.string.add_media", folder);
        int text = dialog.indexOf("R.string.add_text", media);
        int clipboard = dialog.indexOf("R.string.add_clipboard", text);
        assertTrue(file >= 0 && file < folder && folder < media && media < text && text < clipboard);
        assertTrue(chinese.contains("想加入什么内容？"));
        assertTrue(dialog.contains("R.drawable.ic_send_file"));
        assertTrue(dialog.contains("R.drawable.ic_send_clipboard"));
    }

    @Test
    public void removeAndClearSynchronizeAndDeleteOnlyOwnedFiles() throws Exception {
        String send = source("SendController.java");
        int removeStart = send.indexOf("private void removeItem");
        int syncStart = send.indexOf("private void syncSource", removeStart);
        String mutations = send.substring(removeStart, syncStart);

        assertTrue(mutations.contains("syncSource();"));
        assertTrue(mutations.contains("deleteOwned(item);"));
        assertTrue(mutations.contains("GeneratedSendFiles.deleteOwned"));
        assertTrue(mutations.contains("shareItems.clear();"));
        assertTrue(mutations.contains("host.requestRender();"));
        assertFalse(mutations.contains("if (session == null)"));
    }

    @Test
    public void vectorAssetsExistForEveryPickerKindAndVideoOverlay() {
        for (String name : new String[]{"file", "folder", "media", "text", "clipboard", "play"}) {
            Path path = Paths.get("src/main/res/drawable/ic_send_" + name + ".xml");
            assertTrue("missing " + path, Files.exists(path));
        }
    }

    @Test
    public void mediaRowsLoadThumbnailsAndVideoRowsAddPlayOverlay() throws Exception {
        String send = source("SendController.java");

        assertTrue(send.contains("SendThumbnailLoader"));
        assertTrue(send.contains("thumbnailLoader.load"));
        assertTrue(send.contains("SendThumbnailLoader.isCurrent"));
        assertTrue(send.contains("R.drawable.ic_send_play"));
        assertTrue(send.contains("FrameLayout"));
    }

    private static String source(String fileName) throws Exception {
        return read("src/main/java/cn/threatexpert/gonc/" + fileName);
    }

    private static String resource(String path) throws Exception {
        return read("src/main/res/" + path);
    }

    private static String read(String path) throws Exception {
        return new String(Files.readAllBytes(Paths.get(path)), StandardCharsets.UTF_8);
    }
}
