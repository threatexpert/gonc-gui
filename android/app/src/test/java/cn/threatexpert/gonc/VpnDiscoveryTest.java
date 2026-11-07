package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

public class VpnDiscoveryTest {
    @Test public void disabledKeepsExistingArgumentsAndDoesNotAcquireMulticast() {
        assertEquals("", VpnDiscovery.extraArgs(null, false));
        assertEquals("-plain", VpnDiscovery.extraArgs(" -plain ", false));
        assertNull(VpnDiscovery.acquireMulticast(null, false, message -> {
            throw new AssertionError("disabled discovery should not attempt multicast");
        }));
    }

    @Test public void enabledAddsLanDiscoveryWithoutChangingQuotedArguments() {
        assertEquals("-p2p-with-lan", VpnDiscovery.extraArgs(null, true));
        assertEquals("-x \"socks5://with space\" -p2p-with-lan",
                VpnDiscovery.extraArgs("-x \"socks5://with space\"", true));
    }
}
