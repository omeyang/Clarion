import { useRef } from 'react';
import { Tag, Space } from 'antd';
import { ProTable, type ActionType, type ProColumns } from '@ant-design/pro-components';
import { useNavigate } from 'react-router-dom';
import { listCalls } from '@/api/calls';
import type { CallListItem } from '@/types/api';

const statusMap: Record<string, { color: string; text: string }> = {
  pending: { color: 'default', text: '等待中' },
  ringing: { color: 'blue', text: '振铃中' },
  answered: { color: 'green', text: '已接通' },
  completed: { color: 'cyan', text: '已完成' },
  failed: { color: 'red', text: '失败' },
  no_answer: { color: 'orange', text: '未接' },
  busy: { color: 'volcano', text: '占线' },
};

const gradeMap: Record<string, { color: string; text: string }> = {
  high: { color: 'green', text: '高意向' },
  medium: { color: 'blue', text: '中意向' },
  low: { color: 'orange', text: '低意向' },
  none: { color: 'default', text: '无意向' },
};

export default function CallList() {
  const actionRef = useRef<ActionType>();
  const navigate = useNavigate();

  const columns: ProColumns<CallListItem>[] = [
    { title: 'ID', dataIndex: 'id', width: 60, search: false },
    { title: '会话ID', dataIndex: 'session_id', width: 200, ellipsis: true, copyable: true },
    { title: '联系人ID', dataIndex: 'contact_id', width: 90 },
    { title: '任务ID', dataIndex: 'task_id', width: 80 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      valueEnum: Object.fromEntries(Object.entries(statusMap).map(([k, v]) => [k, { text: v.text }])),
      render: (_, r) => {
        const s = statusMap[r.status] || { color: 'default', text: r.status };
        return <Tag color={s.color}>{s.text}</Tag>;
      },
    },
    {
      title: '接听类型',
      dataIndex: 'answer_type',
      width: 90,
      search: false,
      render: (_, r) => r.answer_type || '-',
    },
    {
      title: '通话时长',
      dataIndex: 'duration',
      width: 90,
      search: false,
      render: (_, r) => r.duration ? `${r.duration}s` : '-',
    },
    {
      title: '意向等级',
      dataIndex: 'result_grade',
      width: 90,
      search: false,
      render: (_, r) => {
        if (!r.result_grade) return '-';
        const g = gradeMap[r.result_grade] || { color: 'default', text: r.result_grade };
        return <Tag color={g.color}>{g.text}</Tag>;
      },
    },
    { title: '创建时间', dataIndex: 'created_at', valueType: 'dateTime', width: 170, search: false },
    {
      title: '操作',
      width: 80,
      search: false,
      render: (_, record) => (
        <Space>
          <a onClick={() => navigate(`/calls/${record.id}`)}>详情</a>
        </Space>
      ),
    },
  ];

  return (
    <ProTable<CallListItem>
      actionRef={actionRef}
      columns={columns}
      rowKey="id"
      headerTitle="通话记录"
      request={async (params) => {
        const res = await listCalls({
          page: params.current,
          page_size: params.pageSize,
          task_id: params.task_id ? Number(params.task_id) : undefined,
          status: params.status,
          contact_id: params.contact_id ? Number(params.contact_id) : undefined,
        });
        return { data: res.items, total: res.total, success: true };
      }}
      pagination={{ defaultPageSize: 20 }}
    />
  );
}
