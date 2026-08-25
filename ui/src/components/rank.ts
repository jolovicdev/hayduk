export interface RankChip {
  label: string;
  hot: boolean;
}

// Most modules rank "normal"; a chip on every leaf would be noise, so normal
// renders nothing and only notable reliability stands out.
export function rankChip(rank: string | undefined): RankChip | null {
  switch ((rank ?? "").toLowerCase()) {
    case "excellent":
      return { label: "excellent", hot: true };
    case "great":
      return { label: "great", hot: true };
    case "good":
    case "average":
    case "low":
    case "manual":
      return { label: rank!.toLowerCase(), hot: false };
    default:
      return null;
  }
}
