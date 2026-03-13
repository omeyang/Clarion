import client from './client';
import type { PaginatedResponse, Template, TemplateListItem, TemplateCreate, TemplateUpdate, Snapshot } from '@/types/api';

export async function listTemplates(params: { page?: number; page_size?: number; status?: string; domain?: string }) {
  const res = await client.get<PaginatedResponse<TemplateListItem>>('/templates', { params });
  return res.data;
}

export async function getTemplate(id: number) {
  const res = await client.get<Template>(`/templates/${id}`);
  return res.data;
}

export async function createTemplate(data: TemplateCreate) {
  const res = await client.post<Template>('/templates', data);
  return res.data;
}

export async function updateTemplate(id: number, data: TemplateUpdate) {
  const res = await client.patch<Template>(`/templates/${id}`, data);
  return res.data;
}

export async function deleteTemplate(id: number) {
  await client.delete(`/templates/${id}`);
}

export async function publishTemplate(id: number) {
  const res = await client.post<Snapshot>(`/templates/${id}/publish`);
  return res.data;
}

export async function listSnapshots(templateId: number) {
  const res = await client.get<Snapshot[]>(`/templates/${templateId}/snapshots`);
  return res.data;
}
