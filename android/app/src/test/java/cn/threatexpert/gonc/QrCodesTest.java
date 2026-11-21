package cn.threatexpert.gonc;

import com.google.zxing.BarcodeFormat;
import com.google.zxing.DecodeHintType;
import com.google.zxing.RGBLuminanceSource;
import com.google.zxing.Result;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.qrcode.QRCodeWriter;
import com.journeyapps.barcodescanner.DefaultDecoderFactory;

import org.json.JSONObject;
import org.junit.Test;

import java.util.Collections;

import static org.junit.Assert.*;

public class QrCodesTest {
    private static final String CHINESE_NAME = "家里 VPN 中文配置";

    private RGBLuminanceSource image(BitMatrix matrix) {
        int width = matrix.getWidth(), height = matrix.getHeight();
        int[] pixels = new int[width * height];
        for (int y = 0; y < height; y++) {
            for (int x = 0; x < width; x++) {
                pixels[y * width + x] = matrix.get(x, y) ? 0xff000000 : 0xffffffff;
            }
        }
        return new RGBLuminanceSource(width, height, pixels);
    }

    private String profilePayload() throws Exception {
        return new JSONObject().put("type", VpnQrPayload.TYPE).put("version", 1)
                .put("profile", new JSONObject().put("name", CHINESE_NAME)
                        .put("passphrase", "中文口令Test123")) .toString();
    }

    private String decode(String payload, DefaultDecoderFactory factory) throws Exception {
        return decode(payload, factory, 260);
    }

    private String decode(String payload, DefaultDecoderFactory factory, int size) throws Exception {
        Result result = factory.createDecoder(Collections.emptyMap())
                .decode(image(QrCodes.encodeMatrix(payload, size)));
        assertNotNull(result);
        assertEquals(BarcodeFormat.QR_CODE, result.getBarcodeFormat());
        assertEquals(payload, result.getText());
        return result.getText();
    }

    @Test public void chineseProfileSurvivesProductionEncoderAndCameraDecoder() throws Exception {
        String decoded = decode(profilePayload(), QrScanPolicy.decoderFactory());
        VpnQrPayload parsed = VpnQrPayload.parse(decoded);
        assertEquals(VpnQrPayload.Kind.PROFILE, parsed.kind);
        assertEquals(CHINESE_NAME, parsed.profile.getString("name"));
        assertEquals("中文口令Test123", parsed.profile.getString("passphrase"));
    }

    @Test public void serverProfileCanBeImportedFromItsInlineQr() throws Exception {
        String payload = VpnQrPayload.encodeServerProfile("  中文口令123  ", true, false);
        String scanned = decode(payload, QrScanPolicy.decoderFactory(), VpnServerController.SHARE_QR_SIZE_DP);
        VpnQrPayload parsed = VpnQrPayload.parse(scanned);
        assertEquals(VpnQrPayload.Kind.PROFILE, parsed.kind);
        VpnProfile imported = VpnProfile.fromJson(parsed.profile, "Fallback");
        assertEquals("gonclink", imported.name);
        assertEquals("中文口令123", imported.passphrase);
        assertEquals("8.8.8.8", imported.dnsServers);
        assertEquals(VpnProfile.DEFAULT_VPN_ROUTES, imported.routeCidrs);
        assertTrue(imported.useUdp);
        assertFalse(imported.p2pWithLan);
        assertTrue(imported.blockDnsLeak);
        assertFalse(imported.disconnectProtection);
        assertFalse(imported.tunnelOnly);

        VpnQrPayload changed = VpnQrPayload.parse(VpnQrPayload.encodeServerProfile("Updated123", false, true));
        assertEquals("Updated123", changed.profile.getString("passphrase"));
        assertFalse(changed.profile.getBoolean("useUdp"));
        assertTrue(changed.profile.getBoolean("p2pWithLan"));
        assertEquals("", VpnQrPayload.encodeServerProfile(" ", false, false));
    }

    @Test public void legacyDefaultCameraDecoderUnderstandsUtf8WithoutHints() throws Exception {
        decode(profilePayload(), new DefaultDecoderFactory());
    }

    @Test public void explicitUtf8ReaderAlsoRemainsCompatible() throws Exception {
        decode(profilePayload(), new DefaultDecoderFactory(
                Collections.singletonList(BarcodeFormat.QR_CODE),
                Collections.singletonMap(DecodeHintType.CHARACTER_SET, "UTF-8"), "UTF-8", 0));
    }

    @Test public void unicodeAndAsciiPassphrasesRoundTrip() throws Exception {
        for (String text : new String[]{"中文口令123", "家庭网络🔒é123", "Test12345", "123456789012"}) {
            decode(text, QrScanPolicy.decoderFactory());
            decode(text, new DefaultDecoderFactory());
        }
    }

    @Test public void regressionDemonstratesOldEncoderLosesChineseBeforeScanning() throws Exception {
        BitMatrix legacy = new QRCodeWriter().encode(CHINESE_NAME, BarcodeFormat.QR_CODE, 260, 260);
        Result result = new DefaultDecoderFactory().createDecoder(Collections.emptyMap()).decode(image(legacy));
        assertNotNull(result);
        assertNotEquals(CHINESE_NAME, result.getText());
        assertTrue(result.getText().contains("?"));
    }
}
