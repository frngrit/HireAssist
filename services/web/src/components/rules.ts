import type { Criterion } from '../api/types'

/** Whether a criteria list can be submitted: at least one, each named and positively weighted. */
export const criteriaValid = (criteria: Criterion[]) =>
  criteria.length > 0 && criteria.every((c) => c.name.trim() && c.weight > 0)

/** Green above 70, yellow above 45, red below — the same thresholds everywhere a score shows. */
export const scoreColor = (score: number) => (score >= 70 ? 'green' : score >= 45 ? 'yellow' : 'red')
