package cn.threatexpert.gonc;

import org.junit.Test;

import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URL;
import java.net.URLConnection;
import java.net.URLStreamHandler;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;

public class HttpReceiverProxyTest {
    @Test
    public void openForcesDirectConnectionAndKeepsTimeouts() throws Exception {
        AtomicReference<Proxy> selectedProxy = new AtomicReference<>();
        URL url = new URL(null, "http://127.0.0.1:12345/files", new URLStreamHandler() {
            @Override
            protected URLConnection openConnection(URL ignored) throws IOException {
                throw new AssertionError("no-argument proxy selection must not be used");
            }

            @Override
            protected URLConnection openConnection(URL target, Proxy proxy) {
                selectedProxy.set(proxy);
                return new RecordingConnection(target);
            }
        });

        HttpURLConnection connection = HttpReceiver.open(url);

        assertSame(Proxy.NO_PROXY, selectedProxy.get());
        assertEquals(10000, connection.getConnectTimeout());
        assertEquals(30000, connection.getReadTimeout());
    }

    private static final class RecordingConnection extends HttpURLConnection {
        RecordingConnection(URL url) {
            super(url);
        }

        @Override public void disconnect() {}
        @Override public boolean usingProxy() { return false; }
        @Override public void connect() {}
    }
}
