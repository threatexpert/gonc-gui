package cn.threatexpert.gonc;

import java.util.Objects;

final class ReceivedTargetCheckGuard {
    private ReceivedTargetCheckGuard() {
    }

    static boolean isCurrent(
            long expectedRunId,
            long currentRunId,
            long expectedCheckId,
            long currentCheckId,
            String expectedPath,
            String currentPath,
            String expectedTree,
            String currentTree,
            String expectedSaveLabel,
            String currentSaveLabel,
            boolean shutdown) {
        return !shutdown
                && expectedRunId == currentRunId
                && expectedCheckId == currentCheckId
                && Objects.equals(expectedPath, currentPath)
                && Objects.equals(expectedTree, currentTree)
                && Objects.equals(expectedSaveLabel, currentSaveLabel);
    }
}
