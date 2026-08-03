/**
 * Spectral Discipline: every provider is a wavelength of the one dispersed
 * beam (docs/design/PHILOSOPHY.md). Maps a provider name to a stable band
 * index 1..7 matching the --spectral-N tokens.
 */

/** Canonical band assignments for the providers on PLATE I. */
const PLATE_I: Record<string, number> = {
  anthropic: 1,
  openai: 2,
  google: 3,
  moonshot: 4,
  deepseek: 5,
  mistral: 6,
  ollama: 7,
}

export function spectralIndex(provider: string): number {
  const key = provider.toLowerCase()
  if (PLATE_I[key]) return PLATE_I[key]
  let h = 0
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0
  return (h % 7) + 1
}
