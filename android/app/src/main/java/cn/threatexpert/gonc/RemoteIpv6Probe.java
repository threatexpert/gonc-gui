package cn.threatexpert.gonc;

import java.io.DataInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.Socket;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.concurrent.Callable;
import java.util.concurrent.CompletionService;
import java.util.concurrent.ExecutorCompletionService;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.SSLSocketFactory;

final class RemoteIpv6Probe {
    static final int DEFAULT_TIMEOUT_MS = 5000;

    static final class Result {
        final boolean available;
        final String detail;

        Result(boolean available, String detail) {
            this.available = available;
            this.detail = detail == null ? "" : detail;
        }
    }

    private interface Probe {
        String label();

        void run(SocksEndpoint proxy, int timeoutMs) throws Exception;
    }

    private static final Probe[] PROBES = new Probe[]{
            dnsProbe("2001:4860:4860::8888", 53),
            dnsProbe("2620:fe::fe", 53),
            dnsProbe("2400:3200::1", 53),
            tlsProbe("2400:3200:baba::1", 443)
    };

    private RemoteIpv6Probe() {
    }

    static Result check(String socks5Endpoint, int timeoutMs) {
        SocksEndpoint proxy;
        try {
            proxy = SocksEndpoint.parse(socks5Endpoint);
        } catch (Exception error) {
            return new Result(false, "invalid SOCKS5 endpoint: " + message(error));
        }

        int cleanTimeoutMs = Math.max(1000, timeoutMs);
        ExecutorService executor = Executors.newFixedThreadPool(PROBES.length);
        CompletionService<Result> completion = new ExecutorCompletionService<>(executor);
        for (Probe probe : PROBES) {
            completion.submit(new Callable<Result>() {
                @Override
                public Result call() {
                    try {
                        probe.run(proxy, cleanTimeoutMs);
                        return new Result(true, probe.label());
                    } catch (Throwable error) {
                        return new Result(false, probe.label() + ": " + message(error));
                    }
                }
            });
        }

        long deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(cleanTimeoutMs);
        List<String> failures = new ArrayList<>();
        try {
            for (int i = 0; i < PROBES.length; i++) {
                long remainingNs = deadline - System.nanoTime();
                if (remainingNs <= 0) {
                    break;
                }
                java.util.concurrent.Future<Result> future = completion.poll(remainingNs, TimeUnit.NANOSECONDS);
                if (future == null) {
                    break;
                }
                Result result = future.get();
                if (result.available) {
                    return result;
                }
                failures.add(result.detail);
            }
        } catch (Exception error) {
            failures.add(message(error));
        } finally {
            executor.shutdownNow();
        }
        String detail = failures.isEmpty() ? "timeout" : joinFailures(failures);
        return new Result(false, detail);
    }

    private static Probe dnsProbe(String host, int port) {
        return new Probe() {
            @Override
            public String label() {
                return "[" + host + "]:" + port + " DNS";
            }

            @Override
            public void run(SocksEndpoint proxy, int timeoutMs) throws Exception {
                Socket socket = connectSocks5(proxy, host, port, timeoutMs);
                try {
                    socket.setSoTimeout(timeoutMs);
                    byte[] query = dnsQuery();
                    OutputStream out = socket.getOutputStream();
                    out.write((query.length >>> 8) & 0xff);
                    out.write(query.length & 0xff);
                    out.write(query);
                    out.flush();

                    DataInputStream in = new DataInputStream(socket.getInputStream());
                    int length = in.readUnsignedShort();
                    if (length < 12 || length > 4096) {
                        throw new IOException("invalid DNS response length " + length);
                    }
                    byte[] response = new byte[length];
                    in.readFully(response);
                    int queryId = ((query[0] & 0xff) << 8) | (query[1] & 0xff);
                    int responseId = ((response[0] & 0xff) << 8) | (response[1] & 0xff);
                    if (responseId != queryId) {
                        throw new IOException("DNS response id mismatch");
                    }
                    if ((response[2] & 0x80) == 0) {
                        throw new IOException("DNS response QR bit missing");
                    }
                } finally {
                    closeQuietly(socket);
                }
            }
        };
    }

    private static Probe tlsProbe(String host, int port) {
        return new Probe() {
            @Override
            public String label() {
                return "[" + host + "]:" + port + " TLS";
            }

            @Override
            public void run(SocksEndpoint proxy, int timeoutMs) throws Exception {
                Socket socket = connectSocks5(proxy, host, port, timeoutMs);
                SSLSocket ssl = null;
                try {
                    socket.setSoTimeout(timeoutMs);
                    SSLSocketFactory factory = SSLContext.getDefault().getSocketFactory();
                    ssl = (SSLSocket) factory.createSocket(socket, host, port, true);
                    ssl.setSoTimeout(timeoutMs);
                    ssl.startHandshake();
                } finally {
                    if (ssl != null) {
                        closeQuietly(ssl);
                    } else {
                        closeQuietly(socket);
                    }
                }
            }
        };
    }

    private static Socket connectSocks5(SocksEndpoint proxy, String targetHost, int targetPort, int timeoutMs) throws Exception {
        Socket socket = new Socket();
        socket.connect(new java.net.InetSocketAddress(proxy.host, proxy.port), timeoutMs);
        socket.setSoTimeout(timeoutMs);
        try {
            InputStream in = socket.getInputStream();
            OutputStream out = socket.getOutputStream();
            out.write(new byte[]{0x05, 0x01, 0x00});
            out.flush();
            byte[] response = readFully(in, 2);
            if (response[0] != 0x05 || response[1] != 0x00) {
                throw new IOException("SOCKS5 auth rejected");
            }

            byte[] address = InetAddress.getByName(targetHost).getAddress();
            if (address.length != 16) {
                throw new IOException("target is not IPv6");
            }
            byte[] request = new byte[4 + address.length + 2];
            request[0] = 0x05;
            request[1] = 0x01;
            request[2] = 0x00;
            request[3] = 0x04;
            System.arraycopy(address, 0, request, 4, address.length);
            request[20] = (byte) ((targetPort >>> 8) & 0xff);
            request[21] = (byte) (targetPort & 0xff);
            out.write(request);
            out.flush();

            byte[] header = readFully(in, 4);
            if (header[0] != 0x05 || header[1] != 0x00) {
                throw new IOException("SOCKS5 connect failed: " + String.format(Locale.ROOT, "0x%02x", header[1] & 0xff));
            }
            int atyp = header[3] & 0xff;
            if (atyp == 0x01) {
                readFully(in, 4 + 2);
            } else if (atyp == 0x04) {
                readFully(in, 16 + 2);
            } else if (atyp == 0x03) {
                int length = in.read();
                if (length < 0) {
                    throw new IOException("SOCKS5 truncated domain response");
                }
                readFully(in, length + 2);
            } else {
                throw new IOException("SOCKS5 invalid address type " + atyp);
            }
            return socket;
        } catch (Throwable error) {
            closeQuietly(socket);
            throw error;
        }
    }

    private static byte[] dnsQuery() {
        byte[] qname = qname("example.com");
        byte[] query = new byte[12 + qname.length + 4];
        int id = (int) (System.nanoTime() & 0xffff);
        query[0] = (byte) ((id >>> 8) & 0xff);
        query[1] = (byte) (id & 0xff);
        query[2] = 0x01; // RD
        query[5] = 0x01; // QDCOUNT
        System.arraycopy(qname, 0, query, 12, qname.length);
        int offset = 12 + qname.length;
        query[offset + 1] = 0x01; // A
        query[offset + 3] = 0x01; // IN
        return query;
    }

    private static byte[] qname(String domain) {
        String[] labels = domain.split("\\.");
        int size = 1;
        for (String label : labels) {
            size += 1 + label.length();
        }
        byte[] result = new byte[size];
        int offset = 0;
        for (String label : labels) {
            byte[] bytes = label.getBytes(StandardCharsets.US_ASCII);
            result[offset++] = (byte) bytes.length;
            System.arraycopy(bytes, 0, result, offset, bytes.length);
            offset += bytes.length;
        }
        result[offset] = 0;
        return result;
    }

    private static byte[] readFully(InputStream in, int length) throws IOException {
        byte[] data = new byte[length];
        int offset = 0;
        while (offset < length) {
            int read = in.read(data, offset, length - offset);
            if (read < 0) {
                throw new IOException("unexpected EOF");
            }
            offset += read;
        }
        return data;
    }

    private static void closeQuietly(Socket socket) {
        try {
            if (socket != null) {
                socket.close();
            }
        } catch (IOException ignored) {
        }
    }

    private static String joinFailures(List<String> failures) {
        StringBuilder builder = new StringBuilder();
        int limit = Math.min(2, failures.size());
        for (int i = 0; i < limit; i++) {
            if (builder.length() > 0) {
                builder.append("; ");
            }
            builder.append(failures.get(i));
        }
        if (failures.size() > limit) {
            builder.append("; ...");
        }
        return builder.toString();
    }

    private static String message(Throwable error) {
        if (error == null) {
            return "";
        }
        String message = error.getMessage();
        return message == null || message.trim().isEmpty() ? error.getClass().getSimpleName() : message;
    }

    private static final class SocksEndpoint {
        final String host;
        final int port;

        SocksEndpoint(String host, int port) {
            this.host = host;
            this.port = port;
        }

        static SocksEndpoint parse(String endpoint) throws Exception {
            String clean = endpoint == null ? "" : endpoint.trim();
            if (clean.startsWith("socks5://")) {
                URI uri = new URI(clean);
                String host = uri.getHost();
                int port = uri.getPort();
                if (host != null && port > 0) {
                    return new SocksEndpoint(host, port);
                }
                clean = clean.substring("socks5://".length());
            }
            int colon = clean.lastIndexOf(':');
            if (colon <= 0 || colon == clean.length() - 1) {
                throw new IOException("missing host or port");
            }
            String host = clean.substring(0, colon);
            if (host.startsWith("[") && host.endsWith("]")) {
                host = host.substring(1, host.length() - 1);
            }
            int port = Integer.parseInt(clean.substring(colon + 1));
            return new SocksEndpoint(host, port);
        }
    }
}
