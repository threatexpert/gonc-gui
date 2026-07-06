package cn.threatexpert.gonc;

import android.app.Activity;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.graphics.Rect;
import android.net.Uri;
import android.os.Bundle;
import android.view.Gravity;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.Toast;

import com.google.zxing.BarcodeFormat;
import com.google.zxing.BinaryBitmap;
import com.google.zxing.DecodeHintType;
import com.google.zxing.LuminanceSource;
import com.google.zxing.MultiFormatReader;
import com.google.zxing.RGBLuminanceSource;
import com.google.zxing.Result;
import com.google.zxing.client.android.Intents;
import com.google.zxing.common.GlobalHistogramBinarizer;
import com.google.zxing.common.HybridBinarizer;
import com.journeyapps.barcodescanner.BarcodeCallback;
import com.journeyapps.barcodescanner.BarcodeResult;
import com.journeyapps.barcodescanner.DecoratedBarcodeView;

import java.io.InputStream;
import java.util.Collections;
import java.util.EnumMap;
import java.util.List;
import java.util.Map;

public final class QrScanActivity extends Activity {
    private static final int REQUEST_PICK_IMAGE = 2001;
    private static final int DECODE_TARGET_SIZE = 480;
    private static final int[] TILE_SIZES = {960, 640, 420};
    private static final double TILE_OVERLAP = 0.35;
    private static final Map<DecodeHintType, Object> QR_HINTS = qrHints();
    private static final Map<DecodeHintType, Object> PURE_QR_HINTS = pureQrHints();
    private static final zxingcpp.BarcodeReader.Options CPP_QR_OPTIONS = cppQrOptions();

    private DecoratedBarcodeView barcodeView;
    private boolean completed;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        FrameLayout root = new FrameLayout(this);
        barcodeView = new DecoratedBarcodeView(this);
        String prompt = getIntent() == null ? null : getIntent().getStringExtra(Intents.Scan.PROMPT_MESSAGE);
        if (prompt != null && !prompt.trim().isEmpty()) {
            barcodeView.setStatusText(prompt);
        }
        barcodeView.decodeContinuous(new BarcodeCallback() {
            @Override
            public void barcodeResult(BarcodeResult result) {
                if (result != null && result.getText() != null) {
                    complete(result.getText());
                }
            }
        });
        root.addView(barcodeView, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT
        ));

        Button gallery = new Button(this);
        gallery.setAllCaps(false);
        gallery.setText(R.string.pick_qr_from_album);
        gallery.setOnClickListener(v -> openAlbum());
        FrameLayout.LayoutParams galleryParams = new FrameLayout.LayoutParams(dp(160), dp(46));
        galleryParams.gravity = Gravity.BOTTOM | Gravity.CENTER_HORIZONTAL;
        galleryParams.setMargins(0, 0, 0, dp(28));
        root.addView(gallery, galleryParams);

        setContentView(root);
    }

    @Override
    protected void onResume() {
        super.onResume();
        if (barcodeView != null) {
            barcodeView.resume();
        }
    }

    @Override
    protected void onPause() {
        if (barcodeView != null) {
            barcodeView.pause();
        }
        super.onPause();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == REQUEST_PICK_IMAGE && resultCode == RESULT_OK && data != null && data.getData() != null) {
            decodeImage(data.getData());
        }
    }

    private void openAlbum() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("image/*");
        startActivityForResult(intent, REQUEST_PICK_IMAGE);
    }

    private void decodeImage(Uri uri) {
        try (InputStream input = getContentResolver().openInputStream(uri)) {
            Bitmap bitmap = BitmapFactory.decodeStream(input);
            if (bitmap == null) {
                Toast.makeText(this, R.string.toast_qr_image_failed, Toast.LENGTH_SHORT).show();
                return;
            }
            String result = decodeBitmap(normalizeBitmap(bitmap));
            if (result != null) {
                complete(result);
                return;
            }
            Toast.makeText(this, R.string.toast_qr_not_found, Toast.LENGTH_SHORT).show();
        } catch (Exception ignored) {
            Toast.makeText(this, R.string.toast_qr_not_found, Toast.LENGTH_SHORT).show();
        }
    }

    private static String decodeBitmap(Bitmap bitmap) {
        Bitmap source = bitmap;
        try {
            String result = decodeSingleBitmap(source);
            if (result != null) {
                return result;
            }
            Bitmap bordered = addWhiteBorder(source);
            try {
                result = decodeSingleBitmap(bordered);
                if (result != null) {
                    return result;
                }
            } finally {
                recycleIfDifferent(bordered, source);
            }
            int smallestSide = Math.min(source.getWidth(), source.getHeight());
            if (smallestSide > 0 && smallestSide < DECODE_TARGET_SIZE) {
                int scale = Math.min(5, (int) Math.ceil((double) DECODE_TARGET_SIZE / smallestSide));
                Bitmap scaled = Bitmap.createScaledBitmap(source, source.getWidth() * scale, source.getHeight() * scale, false);
                try {
                    result = decodeSingleBitmap(scaled);
                    if (result != null) {
                        return result;
                    }
                    Bitmap scaledBordered = addWhiteBorder(scaled);
                    try {
                        result = decodeSingleBitmap(scaledBordered);
                        if (result != null) {
                            return result;
                        }
                    } finally {
                        recycleIfDifferent(scaledBordered, scaled);
                    }
                } finally {
                    recycleIfDifferent(scaled, source);
                }
            }
            return decodeTiles(source);
        } finally {
            if (!source.isRecycled()) {
                source.recycle();
            }
        }
    }

    private static String decodeSingleBitmap(Bitmap bitmap) {
        String cppResult = decodeCppBitmap(bitmap);
        if (cppResult != null) {
            return cppResult;
        }

        int width = bitmap.getWidth();
        int height = bitmap.getHeight();
        int[] pixels = new int[width * height];
        bitmap.getPixels(pixels, 0, width, 0, 0, width, height);
        RGBLuminanceSource source = new RGBLuminanceSource(width, height, pixels);
        Result result = decodeSource(source, QR_HINTS);
        if (result != null) {
            return result.getText();
        }
        result = decodeSource(source.invert(), QR_HINTS);
        return result == null ? null : result.getText();
    }

    private static String decodeCppBitmap(Bitmap bitmap) {
        try {
            zxingcpp.BarcodeReader reader = new zxingcpp.BarcodeReader(CPP_QR_OPTIONS);
            Rect fullImage = new Rect(0, 0, bitmap.getWidth(), bitmap.getHeight());
            List<zxingcpp.BarcodeReader.Result> results = reader.read(bitmap, fullImage, 0);
            for (zxingcpp.BarcodeReader.Result result : results) {
                if (result != null
                        && result.getFormat() == zxingcpp.BarcodeReader.Format.QR_CODE
                        && result.getText() != null
                        && !result.getText().trim().isEmpty()) {
                    return result.getText();
                }
            }
        } catch (Throwable ignored) {
            // Fall back to Java ZXing if the native reader is unavailable on a device.
        }
        return null;
    }

    private static Bitmap normalizeBitmap(Bitmap bitmap) {
        if (bitmap.getConfig() == Bitmap.Config.ARGB_8888 && !bitmap.hasAlpha()) {
            return bitmap;
        }
        Bitmap normalized = Bitmap.createBitmap(bitmap.getWidth(), bitmap.getHeight(), Bitmap.Config.ARGB_8888);
        normalized.eraseColor(0xffffffff);
        android.graphics.Canvas canvas = new android.graphics.Canvas(normalized);
        canvas.drawBitmap(bitmap, 0, 0, null);
        if (!bitmap.isRecycled()) {
            bitmap.recycle();
        }
        return normalized;
    }

    private static Result decodeSource(LuminanceSource source, Map<DecodeHintType, Object> hints) {
        Result result = decodeBinaryBitmap(new BinaryBitmap(new HybridBinarizer(source)), hints);
        if (result != null) {
            return result;
        }
        result = decodeBinaryBitmap(new BinaryBitmap(new GlobalHistogramBinarizer(source)), hints);
        if (result != null) {
            return result;
        }
        result = decodeBinaryBitmap(new BinaryBitmap(new HybridBinarizer(source)), PURE_QR_HINTS);
        if (result != null) {
            return result;
        }
        return decodeBinaryBitmap(new BinaryBitmap(new GlobalHistogramBinarizer(source)), PURE_QR_HINTS);
    }

    private static Result decodeBinaryBitmap(BinaryBitmap bitmap, Map<DecodeHintType, Object> hints) {
        MultiFormatReader reader = new MultiFormatReader();
        try {
            return reader.decode(bitmap, hints);
        } catch (Exception ignored) {
            return null;
        } finally {
            reader.reset();
        }
    }

    private static String decodeTiles(Bitmap source) {
        if (Math.max(source.getWidth(), source.getHeight()) <= TILE_SIZES[0]) {
            return null;
        }
        for (int requestedSize : TILE_SIZES) {
            int tileWidth = Math.min(requestedSize, source.getWidth());
            int tileHeight = Math.min(requestedSize, source.getHeight());
            if (tileWidth < 120 || tileHeight < 120) {
                continue;
            }
            int stepX = Math.max(80, (int) Math.round(tileWidth * (1 - TILE_OVERLAP)));
            int stepY = Math.max(80, (int) Math.round(tileHeight * (1 - TILE_OVERLAP)));
            int y = 0;
            while (true) {
                int x = 0;
                while (true) {
                    Bitmap tile = Bitmap.createBitmap(source, x, y, tileWidth, tileHeight);
                    try {
                        Bitmap bordered = addWhiteBorder(tile);
                        try {
                            String result = decodeSingleBitmap(bordered);
                            if (result != null) {
                                return result;
                            }
                        } finally {
                            recycleIfDifferent(bordered, tile);
                        }
                    } finally {
                        recycleIfDifferent(tile, source);
                    }
                    if (x + tileWidth >= source.getWidth()) {
                        break;
                    }
                    x = Math.min(x + stepX, source.getWidth() - tileWidth);
                }
                if (y + tileHeight >= source.getHeight()) {
                    break;
                }
                y = Math.min(y + stepY, source.getHeight() - tileHeight);
            }
        }
        return null;
    }

    private static Bitmap addWhiteBorder(Bitmap source) {
        int margin = Math.max(16, Math.round(Math.min(source.getWidth(), source.getHeight()) * 0.1f));
        Bitmap bordered = Bitmap.createBitmap(source.getWidth() + margin * 2, source.getHeight() + margin * 2, Bitmap.Config.ARGB_8888);
        bordered.eraseColor(0xffffffff);
        android.graphics.Canvas canvas = new android.graphics.Canvas(bordered);
        canvas.drawBitmap(source, margin, margin, null);
        return bordered;
    }

    private static void recycleIfDifferent(Bitmap bitmap, Bitmap keep) {
        if (bitmap != keep && bitmap != null && !bitmap.isRecycled()) {
            bitmap.recycle();
        }
    }

    private static Map<DecodeHintType, Object> qrHints() {
        EnumMap<DecodeHintType, Object> hints = new EnumMap<>(DecodeHintType.class);
        hints.put(DecodeHintType.POSSIBLE_FORMATS, Collections.singletonList(BarcodeFormat.QR_CODE));
        hints.put(DecodeHintType.TRY_HARDER, Boolean.TRUE);
        hints.put(DecodeHintType.CHARACTER_SET, "UTF-8");
        return Collections.unmodifiableMap(hints);
    }

    private static Map<DecodeHintType, Object> pureQrHints() {
        EnumMap<DecodeHintType, Object> hints = new EnumMap<>(DecodeHintType.class);
        hints.putAll(QR_HINTS);
        hints.put(DecodeHintType.PURE_BARCODE, Boolean.TRUE);
        return Collections.unmodifiableMap(hints);
    }

    private static zxingcpp.BarcodeReader.Options cppQrOptions() {
        zxingcpp.BarcodeReader.Options options = new zxingcpp.BarcodeReader.Options();
        options.setFormats(Collections.singleton(zxingcpp.BarcodeReader.Format.QR_CODE));
        options.setTryHarder(true);
        options.setTryRotate(true);
        options.setTryInvert(true);
        options.setTryDownscale(true);
        options.setTryDenoise(true);
        options.setMaxNumberOfSymbols(1);
        options.setTextMode(zxingcpp.BarcodeReader.TextMode.PLAIN);
        return options;
    }

    private void complete(String value) {
        if (completed || value == null || value.trim().isEmpty()) {
            return;
        }
        completed = true;
        Intent data = new Intent();
        data.putExtra(Intents.Scan.RESULT, value);
        setResult(RESULT_OK, data);
        finish();
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }
}
