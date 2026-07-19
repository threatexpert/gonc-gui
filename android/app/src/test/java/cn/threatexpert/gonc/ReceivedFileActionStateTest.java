package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class ReceivedFileActionStateTest {
    @Test
    public void keepsMarkerButDisablesActionsDuringDownload() {
        assertTrue(ReceivedFileActionState.markerVisible(true));
        assertFalse(ReceivedFileActionState.actionsEnabled(true, true, false));
    }

    @Test
    public void enablesAvailableTargetOnlyWhileIdle() {
        assertTrue(ReceivedFileActionState.actionsEnabled(true, false, false));
        assertFalse(ReceivedFileActionState.actionsEnabled(false, false, false));
    }

    @Test
    public void disablesActionsAfterDownloadUntilCompletionRefreshFinishes() {
        assertFalse(ReceivedFileActionState.actionsEnabled(true, false, true));
        assertTrue(ReceivedFileActionState.ownsCompletionRefresh(6, 6));
        assertFalse(ReceivedFileActionState.ownsCompletionRefresh(7, 6));
        assertFalse(ReceivedFileActionState.ownsCompletionRefresh(0, 0));
    }

    @Test
    public void onlyCurrentLiveWorkerCanBeginOneTerminalRefresh() {
        assertTrue(ReceivedFileActionState.shouldBeginTerminalRefresh(8, 8, true, false));
        assertFalse(ReceivedFileActionState.shouldBeginTerminalRefresh(9, 8, true, false));
        assertFalse(ReceivedFileActionState.shouldBeginTerminalRefresh(8, 8, false, false));
        assertFalse(ReceivedFileActionState.shouldBeginTerminalRefresh(8, 8, true, true));
    }

    @Test
    public void disconnectCannotReconnectUntilWorkerTerminatesAndRefreshFinishes() {
        assertFalse(ReceivedFileActionState.canStartNewConnection(true, false));
        assertFalse(ReceivedFileActionState.canStartNewConnection(false, true));
        assertTrue(ReceivedFileActionState.canStartNewConnection(false, false));
    }
}
