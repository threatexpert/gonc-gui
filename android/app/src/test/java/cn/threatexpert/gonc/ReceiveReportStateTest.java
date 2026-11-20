package cn.threatexpert.gonc;
import org.junit.Test;
import static org.junit.Assert.*;

public class ReceiveReportStateTest {
 @Test public void onlyExplicitHelloTimeoutIsRecognized() {
  assertTrue(ReceiveReportState.isWaitTimeout("error:hello-timeout"));
  for(String s:new String[]{null,"error:hello-timeout network","error:MQTT subscription timeout","error:handshake timeout","wait"}) assertFalse(ReceiveReportState.isWaitTimeout(s));
 }
 @Test public void waitingTimeoutKeepsQrAvailable() {
  assertFalse(TransferInlineQrState.retireReceiveQr(false, "error:hello-timeout"));
  assertFalse(TransferInlineQrState.retireReceiveQr(false, "wait-timeout"));
  assertTrue(TransferInlineQrState.showReceiveQr(false, "wait-timeout"));
  assertTrue(TransferInlineQrState.retireReceiveQr(true, "wait-timeout"));
 }
 @Test public void connectedPathRejectsLateLoserAndAllowsReconnect() {
  ReceiveReportState state = new ReceiveReportState();
  assertTrue(state.accept("public", "wait"));
  assertTrue(state.accept("lan", "connected"));
  assertFalse(state.accept("public", "error:hello-timeout"));
  assertFalse(state.accept("public", "wait"));
  assertTrue(state.accept("lan", "disconnected"));
  assertTrue(state.accept("retry", "wait"));
  assertTrue(state.accept("retry", "connected"));
  state.reset();
  assertTrue(state.accept("new-run", "wait"));
 }
}
