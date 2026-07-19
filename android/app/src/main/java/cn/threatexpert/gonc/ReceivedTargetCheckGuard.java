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
            String currentTree) {
        return expectedRunId == currentRunId
                && expectedCheckId == currentCheckId
                && Objects.equals(expectedPath, currentPath)
                && Objects.equals(expectedTree, currentTree);
    }
}
