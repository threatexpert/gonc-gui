package cn.threatexpert.gonc;

import android.app.Activity;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.net.Uri;
import android.os.Bundle;
import android.view.Gravity;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.Toast;

import com.google.zxing.BinaryBitmap;
import com.google.zxing.MultiFormatReader;
import com.google.zxing.RGBLuminanceSource;
import com.google.zxing.Result;
import com.google.zxing.client.android.Intents;
import com.google.zxing.common.HybridBinarizer;
import com.journeyapps.barcodescanner.BarcodeCallback;
import com.journeyapps.barcodescanner.BarcodeResult;
import com.journeyapps.barcodescanner.DecoratedBarcodeView;

import java.io.InputStream;

public final class QrScanActivity extends Activity {
    private static final int REQUEST_PICK_IMAGE = 2001;

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
            int width = bitmap.getWidth();
            int height = bitmap.getHeight();
            int[] pixels = new int[width * height];
            bitmap.getPixels(pixels, 0, width, 0, 0, width, height);
            BinaryBitmap binaryBitmap = new BinaryBitmap(new HybridBinarizer(new RGBLuminanceSource(width, height, pixels)));
            Result result = new MultiFormatReader().decode(binaryBitmap);
            complete(result.getText());
        } catch (Exception ignored) {
            Toast.makeText(this, R.string.toast_qr_not_found, Toast.LENGTH_SHORT).show();
        }
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
