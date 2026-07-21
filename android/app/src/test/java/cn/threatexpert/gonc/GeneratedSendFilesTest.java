package cn.threatexpert.gonc;

import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.TemporaryFolder;

import java.io.ByteArrayInputStream;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.ArrayDeque;
import java.util.Arrays;
import java.util.Queue;
import java.util.TimeZone;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class GeneratedSendFilesTest {
    @Rule
    public TemporaryFolder temp = new TemporaryFolder();

    @Test
    public void textNamesRetryCollisionsAndWriteUtf8() throws Exception {
        File root = temp.newFolder("generated-send");
        Queue<String> tokens = new ArrayDeque<>(Arrays.asList("7f3a", "7f3a", "a92c"));
        GeneratedSendFiles.TokenSource source = tokens::remove;
        TimeZone utc = TimeZone.getTimeZone("UTC");

        File first = GeneratedSendFiles.createText(
                root, "text", "你好", 1784648730000L, utc, source);
        File second = GeneratedSendFiles.createText(
                root, "text", "again", 1784648730000L, utc, source);

        assertEquals("text-20260721-154530-7f3a.txt", first.getName());
        assertEquals("text-20260721-154530-a92c.txt", second.getName());
        assertEquals("你好", new String(Files.readAllBytes(first.toPath()), StandardCharsets.UTF_8));
    }

    @Test
    public void imageCopyPreservesBytesAndMapsSafeExtensions() throws Exception {
        File root = temp.newFolder("generated-send");
        byte[] bytes = new byte[]{1, 3, 5, 7};

        File copied = GeneratedSendFiles.copyImage(
                root,
                "clipboard-image",
                GeneratedSendFiles.extensionForMime("image/png"),
                new ByteArrayInputStream(bytes),
                1784648730000L,
                TimeZone.getTimeZone("UTC"),
                () -> "bb21");

        assertEquals("clipboard-image-20260721-154530-bb21.png", copied.getName());
        assertArrayEquals(bytes, Files.readAllBytes(copied.toPath()));
        assertEquals("jpg", GeneratedSendFiles.extensionForMime("image/jpeg"));
        assertEquals("webp", GeneratedSendFiles.extensionForMime("image/webp"));
        assertEquals("gif", GeneratedSendFiles.extensionForMime("image/gif"));
        assertEquals("bin", GeneratedSendFiles.extensionForMime("image/x-unknown"));
    }

    @Test
    public void deletionIsConstrainedToTheOwnedRoot() throws Exception {
        File root = temp.newFolder("generated-send");
        File owned = new File(root, "clipboard-image.png");
        assertTrue(owned.createNewFile());
        File outside = temp.newFile("user.png");

        assertTrue(GeneratedSendFiles.deleteOwned(root, owned));
        assertFalse(owned.exists());
        assertFalse(GeneratedSendFiles.deleteOwned(root, outside));
        assertTrue(outside.exists());
        assertFalse(GeneratedSendFiles.deleteOwned(root, null));
    }

    @Test
    public void shareItemsDefaultToUserOwnedAndFileUrisOpenDirectly() throws Exception {
        String shareItem = source("ShareItem.java");
        String fileSource = source("AndroidFileSource.java");

        assertTrue(shareItem.contains("private final File ownedFile;"));
        assertTrue(shareItem.contains("File ownedFile()"));
        assertTrue(shareItem.contains("this(uri, displayName, size, mimeType, false, false, 0, null);"));
        assertTrue(fileSource.contains("\"file\".equalsIgnoreCase(uri.getScheme())"));
        assertTrue(fileSource.contains("new FileInputStream(new File(uri.getPath()))"));
    }

    private static String source(String fileName) throws Exception {
        return new String(Files.readAllBytes(Paths.get(
                "src/main/java/cn/threatexpert/gonc/" + fileName)), StandardCharsets.UTF_8);
    }
}
