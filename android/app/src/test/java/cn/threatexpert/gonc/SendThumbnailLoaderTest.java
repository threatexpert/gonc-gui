package cn.threatexpert.gonc;

import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class SendThumbnailLoaderTest {
    @Test
    public void classifiesOnlyImagesAndVideos() {
        assertTrue(SendThumbnailLoader.isImage("image/png"));
        assertTrue(SendThumbnailLoader.isImage("IMAGE/JPEG"));
        assertTrue(SendThumbnailLoader.isVideo("video/mp4"));
        assertFalse(SendThumbnailLoader.isImage("text/plain"));
        assertFalse(SendThumbnailLoader.isVideo("audio/mpeg"));
        assertFalse(SendThumbnailLoader.isImage(null));
    }

    @Test
    public void staleRenderGenerationCannotApply() {
        assertTrue(SendThumbnailLoader.isCurrent(8, 8));
        assertFalse(SendThumbnailLoader.isCurrent(8, 9));
    }

    @Test
    public void implementationIsBoundedAndUsesPlatformThumbnailPaths() throws Exception {
        String source = new String(Files.readAllBytes(Paths.get(
                "src/main/java/cn/threatexpert/gonc/SendThumbnailLoader.java")),
                StandardCharsets.UTF_8);

        assertTrue(source.contains("LruCache<String, Bitmap>"));
        assertTrue(source.contains("Executors.newFixedThreadPool(2)"));
        assertTrue(source.contains("MediaMetadataRetriever"));
        assertTrue(source.contains("loadThumbnail"));
        assertTrue(source.contains("inSampleSize"));
        assertTrue(source.contains("mainHandler.post"));
        assertTrue(source.contains("private volatile long cacheGeneration;"));
    }
}
