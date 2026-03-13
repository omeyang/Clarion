import client from './client';
import type { PaginatedResponse, Call, CallListItem, DialogueTurn, CallEvent } from '@/types/api';

export async function listCalls(params: {
  page?: number;
  page_size?: number;
  task_id?: number;
  status?: string;
  contact_id?: number;
}) {
  const res = await client.get<PaginatedResponse<CallListItem>>('/calls', { params });
  return res.data;
}

export async function getCall(id: number) {
  const res = await client.get<Call>(`/calls/${id}`);
  return res.data;
}

export async function getCallTurns(callId: number) {
  const res = await client.get<DialogueTurn[]>(`/calls/${callId}/turns`);
  return res.data;
}

export async function getCallEvents(callId: number) {
  const res = await client.get<CallEvent[]>(`/calls/${callId}/events`);
  return res.data;
}
