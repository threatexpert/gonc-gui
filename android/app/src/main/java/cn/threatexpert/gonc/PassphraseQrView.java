package cn.threatexpert.gonc;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.util.LruCache;
import android.view.View;
import android.widget.FrameLayout;
import android.widget.ImageView;

import com.google.zxing.WriterException;

final class PassphraseQrView {
    private static final int MASK_SAMPLE_SIZE = 12;
    private static final int PLACEHOLDER_COLOR = Color.rgb(226, 232, 240);
    private static final int COVER_COLOR = Color.rgb(148, 163, 184);

    private static final LruCache<TransferInlineQrState.BitmapCacheKey, CacheEntry> CACHE =
            new LruCache<TransferInlineQrState.BitmapCacheKey, CacheEntry>(
                    TransferInlineQrState.qrBitmapCacheBudgetBytes()) {
                @Override
                protected int sizeOf(TransferInlineQrState.BitmapCacheKey key, CacheEntry value) {
                    return Math.max(1, value.bitmap.getAllocationByteCount());
                }
            };

    private PassphraseQrView() {
    }

    static View create(Context context, UiKit ui, String passphrase, int sizeDp,
                       boolean masked, Runnable onClick, Runnable onError) {
        int displayPixelSize = Math.max(1, ui.dp(sizeDp));
        int encodedPixelSize = TransferInlineQrState.capQrPixelSize(displayPixelSize);
        String cleanPassphrase = passphrase == null ? "" : passphrase.trim();
        TransferInlineQrState.BitmapCacheKey key =
                TransferInlineQrState.productionCacheKey(cleanPassphrase, encodedPixelSize, masked);
        CacheEntry entry = CACHE.get(key);
        boolean newlyFailed = false;
        if (entry == null) {
            entry = createEntry(cleanPassphrase, encodedPixelSize, masked);
            CACHE.put(key, entry);
            newlyFailed = entry.failed;
        }
        if (newlyFailed && onError != null) {
            onError.run();
        }

        ImageView image = new ImageView(context);
        image.setAdjustViewBounds(true);
        image.setScaleType(ImageView.ScaleType.FIT_CENTER);
        image.setImageBitmap(entry.bitmap);

        boolean actionable = !cleanPassphrase.isEmpty() && !entry.failed;
        FrameLayout container = new FrameLayout(context);
        int padding = ui.dp(6);
        container.setPadding(padding, padding, padding, padding);
        container.setBackground(ui.rounded(Color.WHITE, ui.dp(8), Color.rgb(216, 226, 238), 1));
        container.setEnabled(actionable);
        container.setClickable(actionable);
        container.setFocusable(actionable);
        container.setImportantForAccessibility(actionable
                ? View.IMPORTANT_FOR_ACCESSIBILITY_AUTO
                : View.IMPORTANT_FOR_ACCESSIBILITY_NO);
        if (actionable) {
            container.setContentDescription(context.getString(R.string.view_passphrase_qr));
            container.setOnClickListener(view -> {
                if (onClick != null) {
                    onClick.run();
                }
            });
        }
        container.addView(image, new FrameLayout.LayoutParams(displayPixelSize, displayPixelSize));
        return container;
    }

    static void clearCache() {
        CACHE.evictAll();
    }

    private static CacheEntry createEntry(String passphrase, int pixelSize, boolean masked) {
        if (passphrase.isEmpty()) {
            return new CacheEntry(placeholder(pixelSize), false);
        }
        try {
            Bitmap clear = QrCodes.encode(passphrase, pixelSize);
            return new CacheEntry(masked ? masked(clear, pixelSize) : clear, false);
        } catch (WriterException error) {
            return new CacheEntry(placeholder(pixelSize), true);
        }
    }

    private static Bitmap masked(Bitmap clear, int pixelSize) {
        int sampleSize = Math.max(1, Math.min(MASK_SAMPLE_SIZE, pixelSize));
        Bitmap sampled = Bitmap.createScaledBitmap(clear, sampleSize, sampleSize, true);
        Bitmap enlarged = Bitmap.createScaledBitmap(sampled, pixelSize, pixelSize, true);
        Bitmap result = enlarged.copy(Bitmap.Config.ARGB_8888, true);
        Canvas canvas = new Canvas(result);
        int inset = pixelSize / 4;
        Paint cover = new Paint();
        cover.setColor(COVER_COLOR);
        cover.setStyle(Paint.Style.FILL);
        canvas.drawRect(inset, inset, pixelSize - inset, pixelSize - inset, cover);
        return result;
    }

    private static Bitmap placeholder(int pixelSize) {
        Bitmap bitmap = Bitmap.createBitmap(pixelSize, pixelSize, Bitmap.Config.ARGB_8888);
        Canvas canvas = new Canvas(bitmap);
        canvas.drawColor(PLACEHOLDER_COLOR);
        Paint mark = new Paint();
        mark.setColor(COVER_COLOR);
        mark.setStyle(Paint.Style.FILL);
        int inset = pixelSize / 3;
        canvas.drawRect(inset, inset, pixelSize - inset, pixelSize - inset, mark);
        return bitmap;
    }

    private static final class CacheEntry {
        final Bitmap bitmap;
        final boolean failed;

        CacheEntry(Bitmap bitmap, boolean failed) {
            this.bitmap = bitmap;
            this.failed = failed;
        }
    }
}
