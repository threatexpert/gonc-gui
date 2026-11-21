package cn.threatexpert.gonc;

import android.graphics.Bitmap;
import android.graphics.Color;

import com.google.zxing.BarcodeFormat;
import com.google.zxing.EncodeHintType;
import com.google.zxing.WriterException;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.qrcode.QRCodeWriter;

import java.util.Collections;

final class QrCodes {
    private QrCodes() {
    }

    static Bitmap encode(String value, int size) throws WriterException {
        BitMatrix matrix = encodeMatrix(value, size);
        int width = matrix.getWidth();
        int height = matrix.getHeight();
        Bitmap bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888);
        for (int y = 0; y < height; y++) {
            for (int x = 0; x < width; x++) {
                bitmap.setPixel(x, y, matrix.get(x, y) ? Color.BLACK : Color.WHITE);
            }
        }
        return bitmap;
    }

    static BitMatrix encodeMatrix(String value, int size) throws WriterException {
        // UTF-8 also emits an ECI marker so existing readers can decode Unicode
        // without an out-of-band charset hint. The default encoding loses Chinese.
        return new QRCodeWriter().encode(value, BarcodeFormat.QR_CODE, size, size,
                Collections.singletonMap(EncodeHintType.CHARACTER_SET, "UTF-8"));
    }
}
