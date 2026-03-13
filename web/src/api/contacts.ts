import client from './client';
import type { PaginatedResponse, Contact, ContactCreate, ContactBatchResult } from '@/types/api';

export async function listContacts(params: {
  page?: number;
  page_size?: number;
  source?: string;
  status?: string;
  do_not_call?: boolean;
}) {
  const res = await client.get<PaginatedResponse<Contact>>('/contacts', { params });
  return res.data;
}

export async function getContact(id: number) {
  const res = await client.get<Contact>(`/contacts/${id}`);
  return res.data;
}

export async function createContact(data: ContactCreate) {
  const res = await client.post<Contact>('/contacts', data);
  return res.data;
}

export async function batchCreateContacts(items: ContactCreate[]) {
  const res = await client.post<ContactBatchResult>('/contacts/batch', { items });
  return res.data;
}

export async function updateContact(id: number, data: Partial<Contact>) {
  const res = await client.patch<Contact>(`/contacts/${id}`, data);
  return res.data;
}
