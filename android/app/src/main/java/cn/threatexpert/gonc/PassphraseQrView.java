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
        int padding = ui.dp(TransferInlineQrState.inlineQrFramePaddingDp());
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
        if (masked) {
            return new CacheEntry(decorativeMaskedQr(pixelSize), false);
        }
        try {
            return new CacheEntry(QrCodes.encode(passphrase, pixelSize), false);
        } catch (WriterException error) {
            return new CacheEntry(placeholder(pixelSize), true);
        }
    }

    private static Bitmap decorativeMaskedQr(int pixelSize) {
        Bitmap pattern = Bitmap.createBitmap(29, 29, Bitmap.Config.ARGB_8888);
        Canvas canvas = new Canvas(pattern);
        canvas.drawColor(Color.WHITE);
        Paint ink = new Paint();
        ink.setColor(Color.rgb(16, 24, 38));
        int seed = 0x51f15e;
        for (int y = 0; y < 29; y++) {
            for (int x = 0; x < 29; x++) {
                seed = seed * 1103515245 + 12345;
                if (((seed >>> 28) & 1) == 1) {
                    canvas.drawRect(x, y, x + 1, y + 1, ink);
                }
            }
        }
        Bitmap sampled = Bitmap.createScaledBitmap(
                pattern, Math.min(MASK_SAMPLE_SIZE, pixelSize),
                Math.min(MASK_SAMPLE_SIZE, pixelSize), true);
        return Bitmap.createScaledBitmap(sampled, pixelSize, pixelSize, true);
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
