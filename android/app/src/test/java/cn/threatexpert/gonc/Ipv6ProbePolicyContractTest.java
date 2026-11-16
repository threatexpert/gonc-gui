package cn.threatexpert.gonc;

import org.junit.Test;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;
import static org.junit.Assert.*;

public class Ipv6ProbePolicyContractTest {
    @Test public void mobilePolicyIsAppliedAtStartAndOnEveryReconnect() throws Exception {
        String source = new String(Files.readAllBytes(Paths.get("src/main/java/cn/threatexpert/gonc/GoncVpnService.java")), StandardCharsets.UTF_8).replace("\r\n", "\n");
        assertTrue(source.contains("VpnRoutePolicy.rejectIpv6(routeCidrs, effectiveIpv6)"));
        assertTrue(source.contains("Mobilegonc.startTun2SocksWithPolicy(fd, endpoint, \"tun0\", mtu, \"warn\", true, rejectIpv6)"));
        assertFalse(source.contains("Mobilegonc.startTun2Socks("));
        int begin = source.indexOf("private void monitorIpv6Reconnects(");
        int end = source.indexOf("private String waitForSocks5Ready", begin);
        String monitor = source.substring(begin, end);
        assertTrue(monitor.contains("installInterface(true, nextDns, routes, mtu, endpoint, rejectIpv6)"));
        assertTrue(monitor.contains("} else {\n                restartEngine(endpoint, mtu, rejectIpv6);"));
        assertTrue(monitor.indexOf("revision != ready.revision()") < monitor.indexOf("restartEngine("));
        assertTrue(monitor.indexOf("if (isStopRequested()) return;") < monitor.indexOf("restartEngine("));
        begin = source.indexOf("private void restartEngine(");
        end = source.indexOf("private List<String> selectedDns", begin);
        String restart = source.substring(begin, end);
        assertFalse(restart.contains("establishInterface("));
        assertTrue(restart.contains("synchronized (engineLock)"));
        assertTrue(restart.contains("closeDetachedFd(fd)"));
        assertTrue(restart.indexOf("Mobilegonc.stopTun2Socks()") < restart.indexOf("Mobilegonc.startTun2SocksWithPolicy("));
        assertTrue(restart.indexOf("Mobilegonc.startTun2SocksWithPolicy(") < restart.indexOf("tun2socksRunning = true"));
        assertTrue(restart.indexOf("tun2socksRunning = true") < restart.indexOf("markConnected()"));
    }

    @Test public void onlyNewReadyGenerationTriggersAnotherProbe() throws Exception {
        String source = new String(Files.readAllBytes(Paths.get("src/main/java/cn/threatexpert/gonc/GoncVpnService.java")), StandardCharsets.UTF_8);
        assertTrue(source.contains("monitorIpv6Reconnects(tunnelReady, probeRevision"));
        int begin = source.indexOf("private void monitorIpv6Reconnects(");
        int end = source.indexOf("private String waitForSocks5Ready", begin);
        String monitor = source.substring(begin, end);
        assertTrue(monitor.contains("if (currentRevision == revision) continue;"));
        assertTrue(monitor.indexOf("if (currentRevision == revision) continue;") < monitor.indexOf("resolveEffectiveIpv6("));
        assertFalse(monitor.contains("nextCheck"));
        assertFalse(monitor.contains("elapsedRealtime"));
        assertFalse(source.contains("retry in 30 seconds"));
        String zh = new String(Files.readAllBytes(Paths.get("src/main/res/values-zh/strings.xml")), StandardCharsets.UTF_8);
        String en = new String(Files.readAllBytes(Paths.get("src/main/res/values/strings.xml")), StandardCharsets.UTF_8);
        assertTrue(zh.contains("name=\"peer_ipv6_unavailable\">不可用</string>"));
        assertTrue(en.contains("name=\"peer_ipv6_unavailable\">Unavailable</string>"));
    }
}
