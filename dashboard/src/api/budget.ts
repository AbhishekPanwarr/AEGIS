import { apiGet, apiPut } from './client';

export interface BudgetNode {
  id: string;
  parent_id?: string;
  label: string;
  currency: string;
  cap_minor: number;
}

export interface BudgetUsage {
  cap_minor: number;
  committed_minor: number;
  reserved_minor: number;
  headroom_minor: number;
}

export const budgetApi = {
  getNodes: () => apiGet<BudgetNode[]>('/v1/budget-nodes'),
  getUsage: (id: string) => apiGet<BudgetUsage>(`/v1/budget-nodes/${id}/usage`),
  updateCap: (id: string, capMinor: number) =>
    apiPut<BudgetNode>(`/v1/budget-nodes/${id}`, { cap_minor: capMinor }),
};
