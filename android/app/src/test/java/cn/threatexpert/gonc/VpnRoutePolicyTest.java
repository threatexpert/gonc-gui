package cn.threatexpert.gonc;

import org.junit.Test;
import java.util.Arrays;
import java.util.List;
import static org.junit.Assert.*;

public class VpnRoutePolicyTest {
    @Test public void ipv6IsDerivedFromFinalRoutes() {
        assertTrue(VpnRoutePolicy.hasIpv6(""));
        assertFalse(VpnRoutePolicy.hasIpv6("0.0.0.0/0"));
        assertTrue(VpnRoutePolicy.hasIpv6("2001:db8::/32"));
        List<VpnRoutePolicy.Route> routes = VpnRoutePolicy.routes("");
        assertEquals(3, routes.size());
        assertTrue(routes.get(2).isIpv6());
        assertEquals(0, routes.get(2).prefix);
    }

    @Test public void dnsFailureDoesNotChangeRoutesAndRecoveryRestoresDns() {
        String configured = "2001:4860:4860::8888";
        assertEquals(Arrays.asList("8.8.8.8"), VpnRoutePolicy.dns(configured, false));
        assertEquals(Arrays.asList(configured), VpnRoutePolicy.dns(configured, true));
        assertEquals(Arrays.asList("9.9.9.9"), VpnRoutePolicy.dns("9.9.9.9\n" + configured, false));
        assertTrue(VpnRoutePolicy.routes("::/0").get(0).isIpv6());
    }

    @Test public void invalidRulesFailInsteadOfSilentlyDisappearing() {
        for (String cidr : Arrays.asList("::/129", "10.0.0.0/33", "example.com/24", "invalid", "::ffff:192.0.2.1/128")) {
            try { VpnRoutePolicy.routes(cidr); fail(cidr); }
            catch (IllegalArgumentException expected) { }
        }
        try { VpnRoutePolicy.dns("not-an-ip", false); fail(); }
        catch (IllegalArgumentException expected) { }
    }

    @Test public void hostBitsAreNormalized() {
        VpnRoutePolicy.Route route = VpnRoutePolicy.routes("10.12.3.4/8").get(0);
        assertEquals("10.0.0.0", route.address.getHostAddress());
    }
}
