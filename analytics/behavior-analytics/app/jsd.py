import math

def smoothed_distribution(counts: dict, all_bins: list, alpha: float) -> dict:
    total = sum(counts.values())
    k = len(all_bins)
    return {b: (counts.get(b, 0) + alpha) / (total + alpha * k) for b in all_bins}

def jsd(p: dict, q: dict) -> float:
    m = {b: 0.5 * (p.get(b, 0) + q.get(b, 0)) for b in set(p.keys()).union(q.keys())}
    def kl(a, b):
        return sum(a[x] * math.log2(a[x] / b[x]) for x in a if a.get(x, 0) > 0 and b.get(x, 0) > 0)
    return 0.5 * kl(p, m) + 0.5 * kl(q, m)
