/*
 * HackMe — internal/chain/order_tasks.go InsertOrderTask + economics.go floors.
 * check(n): 1 if manifest would be rejected.
 * Encoding: reward_milli (bits 0-15), difficulty (16-23), target_solves (24-39).
 */
static const int MinDifficultyScore = 1;
static const int MaxDifficultyScore = 100;
static const int maxOrderTargetSolves = 10000;
static const double RewardPerDifficultyUnit = 0.0005;
static const double MinOrderPrepaidHMC = 0.05;

__attribute__((export_name("check"))) int check(long long n) {
    unsigned long long u = (unsigned long long)n;
    int reward_milli = (int)(u & 0xffffu);
    int diff = (int)((u >> 16) & 0xffu);
    int target = (int)((u >> 24) & 0xffffu);

    if (target < 1 || target > maxOrderTargetSolves) {
        return 1;
    }
    if (diff < MinDifficultyScore || diff > MaxDifficultyScore) {
        return 1;
    }
    double reward = (double)reward_milli / 1000.0;
    double min_reward = (double)diff * RewardPerDifficultyUnit;
    if (reward + 1e-12 < min_reward) {
        return 1;
    }
    double prepaid = reward * (double)target;
    if (prepaid + 1e-12 < MinOrderPrepaidHMC) {
        return 1;
    }
    return 0;
}
