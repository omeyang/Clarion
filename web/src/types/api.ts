export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

// Templates
export interface Template {
  id: number;
  name: string;
  domain: string;
  opening_script: string | null;
  state_machine_config: Record<string, unknown> | null;
  extraction_schema: Record<string, unknown> | null;
  grading_rules: Record<string, unknown> | null;
  prompt_templates: Record<string, unknown> | null;
  notification_config: Record<string, unknown> | null;
  call_protection_config: Record<string, unknown> | null;
  precompiled_audios: Record<string, unknown> | null;
  status: string;
  version: number;
  created_at: string;
}

export interface TemplateListItem {
  id: number;
  name: string;
  domain: string;
  status: string;
  version: number;
  created_at: string;
}

export interface TemplateCreate {
  name: string;
  domain: string;
  opening_script?: string;
  state_machine_config?: Record<string, unknown>;
  extraction_schema?: Record<string, unknown>;
  grading_rules?: Record<string, unknown>;
  prompt_templates?: Record<string, unknown>;
  notification_config?: Record<string, unknown>;
  call_protection_config?: Record<string, unknown>;
  precompiled_audios?: Record<string, unknown>;
}

export interface TemplateUpdate extends Partial<TemplateCreate> {}

export interface Snapshot {
  id: number;
  template_id: number;
  snapshot_data: Record<string, unknown>;
  created_at: string;
}

// Tasks
export interface Task {
  id: number;
  name: string;
  scenario_template_id: number;
  template_snapshot_id: number;
  contact_filter: Record<string, unknown> | null;
  schedule_config: Record<string, unknown> | null;
  daily_limit: number;
  max_concurrent: number;
  status: string;
  created_at: string;
}

export interface TaskCreate {
  name: string;
  scenario_template_id: number;
  contact_filter?: Record<string, unknown>;
  schedule_config?: Record<string, unknown>;
  daily_limit?: number;
  max_concurrent?: number;
}

export interface TaskUpdate {
  name?: string;
  contact_filter?: Record<string, unknown>;
  schedule_config?: Record<string, unknown>;
  daily_limit?: number;
  max_concurrent?: number;
}

export type TaskAction = 'start' | 'pause' | 'resume' | 'cancel';

// Contacts
export interface Contact {
  id: number;
  phone_masked: string;
  phone_hash: string;
  source: string | null;
  profile_json: Record<string, unknown> | null;
  current_status: string;
  do_not_call: boolean;
  created_at: string;
}

export interface ContactCreate {
  phone_masked: string;
  phone_hash: string;
  source?: string;
  profile_json?: Record<string, unknown>;
  do_not_call?: boolean;
}

export interface ContactBatchResult {
  total: number;
  created: number;
  skipped: number;
}

// Calls
export interface CallListItem {
  id: number;
  contact_id: number;
  task_id: number;
  session_id: string;
  status: string;
  answer_type: string | null;
  duration: number | null;
  result_grade: string | null;
  created_at: string;
}

export interface Call {
  id: number;
  contact_id: number;
  task_id: number;
  template_snapshot_id: number;
  session_id: string;
  status: string;
  answer_type: string | null;
  duration: number | null;
  record_url: string | null;
  transcript: string | null;
  extracted_fields: Record<string, unknown> | null;
  result_grade: string | null;
  next_action: string | null;
  rule_trace: Record<string, unknown> | null;
  ai_summary: string | null;
  created_at: string;
}

export interface DialogueTurn {
  id: number;
  call_id: number;
  turn_number: number;
  speaker: string;
  content: string;
  state_before: string | null;
  state_after: string | null;
  asr_latency_ms: number | null;
  llm_latency_ms: number | null;
  tts_latency_ms: number | null;
  asr_confidence: number | null;
  is_interrupted: boolean;
  created_at: string;
}

export interface CallEvent {
  id: number;
  call_id: number;
  event_type: string;
  timestamp_ms: number;
  metadata_json: Record<string, unknown> | null;
  created_at: string;
}
