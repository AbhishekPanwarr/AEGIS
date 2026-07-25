import { apiGet } from './client';

export interface Agent {
  id: string;
  group_id: string;
  persona: string;
  status: string;
}

export const identityApi = {
  getAgents: () => apiGet<Agent[]>('/v1/agents'),
};
