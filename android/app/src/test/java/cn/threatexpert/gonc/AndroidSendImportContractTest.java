package cn.threatexpert.gonc;

import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;

import static org.junit.Assert.assertTrue;

public class AndroidSendImportContractTest {
    @Test
    public void hostExposesMediaTextAndClipboardEntries() throws Exception {
        String host = source("ModuleHost.java");

        assertTrue(host.contains("void pickSendMedia();"));
        assertTrue(host.contains("void importSendClipboard();"));
        assertTrue(host.contains("void addAuthoredSendText(String text);"));
    }

    @Test
    public void mediaUsesPhotoPickerWithImageVideoSafFallback() throws Exception {
        String activity = source("MainActivity.java");

        assertTrue(activity.contains("REQUEST_OPEN_SEND_MEDIA"));
        assertTrue(activity.contains("MediaStore.ACTION_PICK_IMAGES"));
        assertTrue(activity.contains("MediaStore.EXTRA_PICK_IMAGES_MAX"));
        assertTrue(activity.contains("Intent.EXTRA_MIME_TYPES"));
        assertTrue(activity.contains("new String[]{\"image/*\", \"video/*\"}"));
        assertTrue(activity.contains("collectPickerUris"));
        assertTrue(activity.contains("mimeType.startsWith(\"image/\")"));
        assertTrue(activity.contains("mimeType.startsWith(\"video/\")"));
    }

    @Test
    public void clipboardPrefersImagesThenFallsBackToText() throws Exception {
        String activity = source("MainActivity.java");
        int image = activity.indexOf("clipboardImageUri");
        int text = activity.indexOf("coerceToText", image);

        assertTrue(image >= 0);
        assertTrue(text > image);
        assertTrue(activity.contains("GeneratedSendFiles.copyImage"));
        assertTrue(activity.contains("GeneratedSendFiles.createText"));
        assertTrue(activity.contains("\"clipboard-image\""));
        assertTrue(activity.contains("\"clipboard-text\""));
    }

    @Test
    public void clipboardFileUrisAreNotCoercedIntoText() throws Exception {
        String activity = source("MainActivity.java");

        assertTrue(activity.contains("if (item.getUri() != null)"));
        assertTrue(activity.contains("continue;\n            }\n            CharSequence coerced = item.coerceToText(this)"));
    }

    @Test
    public void authoredTextAndFailuresHaveLocalizedContracts() throws Exception {
        String activity = source("MainActivity.java");
        String english = resource("values/strings.xml");
        String chinese = resource("values-zh/strings.xml");

        assertTrue(activity.contains("addGeneratedText(\"text\", text)"));
        assertTrue(activity.contains("GeneratedSendFiles.createText(generatedSendRoot(), source, text)"));
        assertTrue(english.contains("Clipboard has no text or image to add"));
        assertTrue(chinese.contains("剪贴板中没有可添加的文字或图片"));
        assertTrue(chinese.contains("已添加可用媒体，另有 %1$d 项无法读取"));
        assertTrue(chinese.contains("无法添加此内容"));
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
