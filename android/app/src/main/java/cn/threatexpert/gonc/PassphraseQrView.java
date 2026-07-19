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
    private static final int CACHE_ENTRIES = 6;
    private static final int MASK_SAMPLE_SIZE = 12;
    private static final int PLACEHOLDER_COLOR = Color.rgb(226, 232, 240);
    private static final int COVER_COLOR = Color.rgb(148, 163, 184);

    private static final LruCache<TransferInlineQrState.CacheKey, BitmapPair> CACHE =
            new LruCache<>(CACHE_ENTRIES);

    private PassphraseQrView() {
    }

    static View create(Context context, UiKit ui, String passphrase, int sizeDp,
                       boolean masked, Runnable onClick) {
        int pixelSize = Math.max(1, ui.dp(sizeDp));
        String cleanPassphrase = passphrase == null ? "" : passphrase.trim();
        TransferInlineQrState.CacheKey key =
                new TransferInlineQrState.CacheKey(cleanPassphrase, pixelSize);
        BitmapPair pair = CACHE.get(key);
        if (pair == null) {
            pair = createBitmaps(cleanPassphrase, pixelSize);
            CACHE.put(key, pair);
        }

        ImageView image = new ImageView(context);
        image.setAdjustViewBounds(true);
        image.setScaleType(ImageView.ScaleType.FIT_CENTER);
        image.setImageBitmap(masked ? pair.masked : pair.clear);

        FrameLayout container = new FrameLayout(context);
        int padding = ui.dp(6);
        container.setPadding(padding, padding, padding, padding);
        container.setBackground(ui.rounded(Color.WHITE, ui.dp(8), Color.rgb(216, 226, 238), 1));
        container.setContentDescription(context.getString(R.string.view_passphrase_qr));
        container.setClickable(true);
        container.setFocusable(true);
        container.setOnClickListener(view -> {
            if (onClick != null) {
                onClick.run();
            }
        });
        container.addView(image, new FrameLayout.LayoutParams(pixelSize, pixelSize));
        return container;
    }

    private static BitmapPair createBitmaps(String passphrase, int pixelSize) {
        if (passphrase.isEmpty()) {
            Bitmap placeholder = placeholder(pixelSize);
            return new BitmapPair(placeholder, placeholder);
        }
        try {
            Bitmap clear = QrCodes.encode(passphrase, pixelSize);
            return new BitmapPair(clear, masked(clear, pixelSize));
        } catch (WriterException error) {
            Bitmap placeholder = placeholder(pixelSize);
            return new BitmapPair(placeholder, placeholder);
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

    private static final class BitmapPair {
        final Bitmap clear;
        final Bitmap masked;

        BitmapPair(Bitmap clear, Bitmap masked) {
            this.clear = clear;
            this.masked = masked;
        }
    }
}
