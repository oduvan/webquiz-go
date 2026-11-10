// Points are shown with at most 2 decimals, and whole numbers without a decimal part.
export function roundPoints(value: unknown): number {
  const number = Number(value);
  if (!isFinite(number)) return 0;
  return Math.round(number * 100) / 100;
}

export function formatPoints(value: unknown): string {
  return String(roundPoints(value));
}
