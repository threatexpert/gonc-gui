package cn.threatexpert.gonc;

import android.content.ContentResolver;
import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.media.MediaMetadataRetriever;
import android.media.ThumbnailUtils;
import android.net.Uri;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.util.LruCache;
import android.util.Size;

import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Locale;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

final class SendThumbnailLoader {
    interface Callback {
        void onLoaded(Bitmap bitmap);
    }

    private static final int CACHE_BYTES = 8 * 1024 * 1024;

    private final Context context;
    private final ContentResolver resolver;
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService executor = Executors.newFixedThreadPool(2);
    private final LruCache<String, Bitmap> cache = new LruCache<String, Bitmap>(CACHE_BYTES) {
        @Override
        protected int sizeOf(String key, Bitmap value) {
            return value.getAllocationByteCount();
        }
    };
    private volatile long cacheGeneration;

    SendThumbnailLoader(Context context) {
        this.context = context.getApplicationContext();
        this.resolver = this.context.getContentResolver();
    }

    static boolean isImage(String mimeType) {
        return mimeType != null && mimeType.toLowerCase(Locale.US).startsWith("image/");
    }

    static boolean isVideo(String mimeType) {
        return mimeType != null && mimeType.toLowerCase(Locale.US).startsWith("video/");
    }

    static boolean isCurrent(long requestedGeneration, long currentGeneration) {
        return requestedGeneration == currentGeneration;
    }

    void load(ShareItem item, int targetPx, long renderGeneration, Callback callback) {
        if ((!isImage(item.mimeType()) && !isVideo(item.mimeType())) || targetPx <= 0) {
            return;
        }
        String key = item.uri() + "|" + item.mimeType() + "|" + targetPx;
        Bitmap cached = cache.get(key);
        if (cached != null) {
            mainHandler.post(() -> callback.onLoaded(cached));
            return;
        }
        long requestedCacheGeneration = cacheGeneration;
        executor.execute(() -> {
            Bitmap bitmap = null;
            try {
                bitmap = isVideo(item.mimeType())
                        ? loadVideo(item.uri(), targetPx)
                        : loadImage(item.uri(), targetPx);
            } catch (IOException | RuntimeException ignored) {
            }
            if (bitmap == null || requestedCacheGeneration != cacheGeneration) {
                return;
            }
            cache.put(key, bitmap);
            Bitmap result = bitmap;
            mainHandler.post(() -> callback.onLoaded(result));
        });
    }

    void clear() {
        cacheGeneration++;
        cache.evictAll();
    }

    private Bitmap loadImage(Uri uri, int targetPx) throws IOException {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && !"file".equals(uri.getScheme())) {
            return resolver.loadThumbnail(uri, new Size(targetPx, targetPx), null);
        }
        BitmapFactory.Options bounds = new BitmapFactory.Options();
        bounds.inJustDecodeBounds = true;
        try (InputStream input = open(uri)) {
            BitmapFactory.decodeStream(input, null, bounds);
        }
        BitmapFactory.Options options = new BitmapFactory.Options();
        options.inSampleSize = sampleSize(bounds.outWidth, bounds.outHeight, targetPx);
        try (InputStream input = open(uri)) {
            Bitmap decoded = BitmapFactory.decodeStream(input, null, options);
            return decoded == null
                    ? null
                    : ThumbnailUtils.extractThumbnail(decoded, targetPx, targetPx,
                    ThumbnailUtils.OPTIONS_RECYCLE_INPUT);
        }
    }

    private Bitmap loadVideo(Uri uri, int targetPx) {
        MediaMetadataRetriever retriever = new MediaMetadataRetriever();
        try {
            if ("file".equals(uri.getScheme())) {
                retriever.setDataSource(uri.getPath());
            } else {
                retriever.setDataSource(context, uri);
            }
            Bitmap frame = retriever.getFrameAtTime(-1, MediaMetadataRetriever.OPTION_CLOSEST_SYNC);
            return frame == null
                    ? null
                    : ThumbnailUtils.extractThumbnail(frame, targetPx, targetPx,
                    ThumbnailUtils.OPTIONS_RECYCLE_INPUT);
        } finally {
            try {
                retriever.release();
            } catch (IOException ignored) {
            }
        }
    }

    private InputStream open(Uri uri) throws IOException {
        if ("file".equals(uri.getScheme())) {
            return new FileInputStream(new File(uri.getPath()));
        }
        InputStream input = resolver.openInputStream(uri);
        if (input == null) {
            throw new IOException("cannot open thumbnail source");
        }
        return input;
    }

    private static int sampleSize(int width, int height, int targetPx) {
        int sample = 1;
        while (width / (sample * 2) >= targetPx * 2
                && height / (sample * 2) >= targetPx * 2) {
            sample *= 2;
        }
        return sample;
    }
}
