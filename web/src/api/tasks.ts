import client from './client';
import type { PaginatedResponse, Task, TaskCreate, TaskUpdate, TaskAction } from '@/types/api';

export async function listTasks(params: { page?: number; page_size?: number; status?: string }) {
  const res = await client.get<PaginatedResponse<Task>>('/tasks', { params });
  return res.data;
}

export async function getTask(id: number) {
  const res = await client.get<Task>(`/tasks/${id}`);
  return res.data;
}

export async function createTask(data: TaskCreate) {
  const res = await client.post<Task>('/tasks', data);
  return res.data;
}

export async function updateTask(id: number, data: TaskUpdate) {
  const res = await client.patch<Task>(`/tasks/${id}`, data);
  return res.data;
}

export async function deleteTask(id: number) {
  await client.delete(`/tasks/${id}`);
}

export async function performTaskAction(id: number, action: TaskAction) {
  const res = await client.post<Task>(`/tasks/${id}/actions`, { action });
  return res.data;
}
