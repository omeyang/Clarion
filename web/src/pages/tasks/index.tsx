import { useRef, useState } from 'react';
import { Button, Tag, Space, Popconfirm, message, Modal, Input, InputNumber, Select } from 'antd';
import { PlusOutlined, PlayCircleOutlined, PauseCircleOutlined, StopOutlined } from '@ant-design/icons';
import { ProTable, type ActionType, type ProColumns } from '@ant-design/pro-components';
import { listTasks, deleteTask, performTaskAction, createTask } from '@/api/tasks';
import { listTemplates } from '@/api/templates';
import type { Task, TemplateListItem } from '@/types/api';
import { useNavigate } from 'react-router-dom';

const statusConfig: Record<string, { color: string; text: string }> = {
  draft: { color: 'default', text: '草稿' },
  pending: { color: 'blue', text: '待执行' },
  running: { color: 'green', text: '运行中' },
  paused: { color: 'orange', text: '已暂停' },
  completed: { color: 'cyan', text: '已完成' },
  cancelled: { color: 'red', text: '已取消' },
};

export default function TaskList() {
  const actionRef = useRef<ActionType>();
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);

  const handleAction = async (id: number, action: 'start' | 'pause' | 'resume' | 'cancel') => {
    await performTaskAction(id, action);
    message.success('操作成功');
    actionRef.current?.reload();
  };

  const columns: ProColumns<Task>[] = [
    { title: 'ID', dataIndex: 'id', width: 60, search: false },
    { title: '任务名称', dataIndex: 'name', ellipsis: true },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      valueEnum: Object.fromEntries(Object.entries(statusConfig).map(([k, v]) => [k, { text: v.text }])),
      render: (_, r) => {
        const s = statusConfig[r.status] || { color: 'default', text: r.status };
        return <Tag color={s.color}>{s.text}</Tag>;
      },
    },
    { title: '日限量', dataIndex: 'daily_limit', width: 80, search: false },
    { title: '并发数', dataIndex: 'max_concurrent', width: 80, search: false },
    { title: '创建时间', dataIndex: 'created_at', valueType: 'dateTime', width: 170, search: false },
    {
      title: '操作',
      width: 240,
      search: false,
      render: (_, record) => (
        <Space>
          <a onClick={() => navigate(`/tasks/${record.id}`)}>详情</a>
          {record.status === 'draft' && (
            <a onClick={() => handleAction(record.id, 'start')}>
              <PlayCircleOutlined /> 启动
            </a>
          )}
          {record.status === 'running' && (
            <a onClick={() => handleAction(record.id, 'pause')}>
              <PauseCircleOutlined /> 暂停
            </a>
          )}
          {record.status === 'paused' && (
            <a onClick={() => handleAction(record.id, 'resume')}>
              <PlayCircleOutlined /> 恢复
            </a>
          )}
          {['running', 'paused'].includes(record.status) && (
            <Popconfirm title="确定取消此任务？" onConfirm={() => handleAction(record.id, 'cancel')}>
              <a style={{ color: '#ff4d4f' }}><StopOutlined /> 取消</a>
            </Popconfirm>
          )}
          {record.status === 'draft' && (
            <Popconfirm
              title="确定删除此任务？"
              onConfirm={async () => {
                await deleteTask(record.id);
                message.success('已删除');
                actionRef.current?.reload();
              }}
            >
              <a style={{ color: '#ff4d4f' }}>删除</a>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <>
      <ProTable<Task>
        actionRef={actionRef}
        columns={columns}
        rowKey="id"
        headerTitle="外呼任务"
        request={async (params) => {
          const res = await listTasks({
            page: params.current,
            page_size: params.pageSize,
            status: params.status,
          });
          return { data: res.items, total: res.total, success: true };
        }}
        pagination={{ defaultPageSize: 20 }}
        toolBarRender={() => [
          <Button key="create" type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            新建任务
          </Button>,
        ]}
      />
      <CreateTaskModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onSuccess={() => {
          setCreateOpen(false);
          actionRef.current?.reload();
        }}
      />
    </>
  );
}

function CreateTaskModal({
  open,
  onClose,
  onSuccess,
}: {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [name, setName] = useState('');
  const [templateId, setTemplateId] = useState<number | undefined>();
  const [dailyLimit, setDailyLimit] = useState(100);
  const [maxConcurrent, setMaxConcurrent] = useState(5);
  const [templates, setTemplates] = useState<TemplateListItem[]>([]);
  const [loading, setLoading] = useState(false);

  const loadTemplates = async () => {
    const res = await listTemplates({ page: 1, page_size: 100, status: 'published' });
    setTemplates(res.items);
  };

  const handleOk = async () => {
    if (!name.trim() || !templateId) {
      message.warning('请填写任务名称并选择模板');
      return;
    }
    setLoading(true);
    try {
      await createTask({
        name: name.trim(),
        scenario_template_id: templateId,
        daily_limit: dailyLimit,
        max_concurrent: maxConcurrent,
      });
      message.success('创建成功');
      setName('');
      setTemplateId(undefined);
      onSuccess();
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title="新建外呼任务"
      open={open}
      onCancel={onClose}
      onOk={handleOk}
      confirmLoading={loading}
      afterOpenChange={(visible) => visible && loadTemplates()}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16, paddingTop: 8 }}>
        <div>
          <label>任务名称</label>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：3月房产初筛批次1" style={{ marginTop: 4 }} />
        </div>
        <div>
          <label>话术模板</label>
          <Select
            value={templateId}
            onChange={setTemplateId}
            placeholder="选择已发布的模板"
            style={{ width: '100%', marginTop: 4 }}
            options={templates.map((t) => ({ label: `${t.name} (${t.domain})`, value: t.id }))}
          />
        </div>
        <div style={{ display: 'flex', gap: 16 }}>
          <div style={{ flex: 1 }}>
            <label>日限量</label>
            <InputNumber value={dailyLimit} onChange={(v) => setDailyLimit(v || 100)} min={1} style={{ width: '100%', marginTop: 4 }} />
          </div>
          <div style={{ flex: 1 }}>
            <label>最大并发</label>
            <InputNumber value={maxConcurrent} onChange={(v) => setMaxConcurrent(v || 5)} min={1} max={20} style={{ width: '100%', marginTop: 4 }} />
          </div>
        </div>
      </div>
    </Modal>
  );
}
