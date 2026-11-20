"""Print the seven-dimension Council results using exact arithmetic."""
import json
import sys
from fractions import Fraction
from pathlib import Path

JUDGES = ("Astra", "Gemini", "Kimi")


def mean(values):
    return sum(values, Fraction()) / len(values)


def overall(ballot):
    if ballot is None:
        return None
    if not isinstance(ballot, list) or not ballot:
        raise ValueError("expected seven scores or a nonempty matrix")
    rows = ballot if isinstance(ballot[0], list) else [ballot]
    page_scores = []
    for row in rows:
        if not isinstance(row, list) or len(row) != 7:
            raise ValueError("each ballot row must contain seven scores")
        scores = [Fraction(str(v)) for v in row]
        if any(v < 0 or v > 10 or (10 * v).denominator != 1 for v in scores):
            raise ValueError("scores must be 0–10 in tenths")
        page_scores.append(mean(scores))
    return mean(page_scores)


def summarize(rounds, threshold):
    ranked = []
    first_pass = None
    print("| Round | Astra | Gemini | Kimi |")
    print("|---:|---:|---:|---:|")
    for number, votes in enumerate(rounds, 1):
        results = [overall(votes[name]) for name in JUDGES]
        displays = ["pending" if x is None else f"{float(x):.4f}" for x in results]
        print(f"| {number} | " + " | ".join(displays) + " |")
        if None in results:
            continue
        minimum = min(results)
        ranked.append((minimum, mean(results), number))
        if minimum >= threshold and first_pass is None:
            first_pass = number
    return first_pass, max(ranked)[2] if ranked else None


if __name__ == "__main__":
    data = json.loads(Path(sys.argv[1]).read_text(), parse_float=str)
    threshold = Fraction(sys.argv[2])
    passed, best = summarize(data["rounds"], threshold)
    print(f"First passing round: {passed}. Best fully scored candidate: {best}.")
