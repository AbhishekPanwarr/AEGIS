import { apiGet, apiPost, apiPut } from './client';

export interface ContainmentLevel {
  level: string;
}

export interface ContainmentState {
  scope_type: string;
  scope_id: string;
  level: string;
  reason?: string;
  actor: string;
}

export interface ContainmentEvent {
  id: number;
  scope_type: string;
  scope_id: string;
  to_level: string;
  from_level?: string;
  reason?: string;
  actor: string;
  epoch_bumped_to?: number;
  created_at: string;
}

export interface EmergencyStopResponse {
  level: string;
  epoch_bumped_to: number;
  sagas_swept: number;
  orphaned_funds_minor: number;
}

export interface ApprovalRequest {
  id: string;
  decision_context: any;
  agent_id: string;
  state: string;
  approver?: string;
  requested_at: string;
}

export const containmentApi = {
  getLevel: (agentId: string, groupId: string, mandateId: string) =>
    apiGet<ContainmentLevel>(`/v1/containment/level?agent_id=${agentId}&group_id=${groupId}&mandate_id=${mandateId}`),

  getStates: () => apiGet<ContainmentState[]>('/v1/containment/states'),

  getEvents: (limit = 20) => apiGet<ContainmentEvent[]>(`/v1/containment/events?limit=${limit}`),

  setLevel: (scopeType: string, scopeId: string, level: string, reason: string, actor: string) =>
    apiPut<any>(`/v1/containment/${scopeType}/${scopeId}`, { level, reason, actor }),

  emergencyStop: (scopeType: string, scopeId: string, reason: string, actor: string) =>
    apiPost<EmergencyStopResponse>('/v1/emergency-stop', { scope_type: scopeType, scope_id: scopeId, reason, actor }),

  getPendingApprovals: () => apiGet<ApprovalRequest[]>('/v1/approvals/pending'),

  decideApproval: (id: string, decision: string, actor: string, rationaleCode: string) =>
    apiPost<ApprovalRequest>(`/v1/approvals/${id}/decide`, { decision, actor, rationale_code: rationaleCode }),
};
