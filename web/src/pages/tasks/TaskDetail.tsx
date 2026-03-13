import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Card, Descriptions, Button, Space, Spin, Tag, message } from 'antd';
import { ArrowLeftOutlined, PlayCircleOutlined, PauseCircleOutlined, StopOutlined } from '@ant-design/icons';
import { getTask, performTaskAction } from '@/api/tasks';
import type { Task } from '@/types/api';
import dayjs from 'dayjs';

const statusConfig: Record<string, { color: string; text: string }> = {
  draft: { color: 'default', text: '草稿' },
  pending: { color: 'blue', text: '待执行' },
  running: { color: 'green', text: '运行中' },
  paused: { color: 'orange', text: '已暂停' },
  completed: { color: 'cyan', text: '已完成' },
  cancelled: { color: 'red', text: '已取消' },
};

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [task, setTask] = useState<Task | null>(null);
  const [loading, setLoading] = useState(true);

  const load = async () => {
    if (!id) return;
    setLoading(true);
    try {
      setTask(await getTask(Number(id)));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, [id]);

  const handleAction = async (action: 'start' | 'pause' | 'resume' | 'cancel') => {
    if (!id) return;
    await performTaskAction(Number(id), action);
    message.success('操作成功');
    load();
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!task) return <div>任务不存在</div>;

  const s = statusConfig[task.status] || { color: 'default', text: task.status };

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/tasks')}>返回列表</Button>
        {task.status === 'draft' && (
          <Button type="primary" icon={<PlayCircleOutlined />} onClick={() => handleAction('start')}>启动</Button>
        )}
        {task.status === 'running' && (
          <Button icon={<PauseCircleOutlined />} onClick={() => handleAction('pause')}>暂停</Button>
        )}
        {task.status === 'paused' && (
          <Button icon={<PlayCircleOutlined />} onClick={() => handleAction('resume')}>恢复</Button>
        )}
        {['running', 'paused'].includes(task.status) && (
          <Button danger icon={<StopOutlined />} onClick={() => handleAction('cancel')}>取消</Button>
        )}
      </Space>

      <Card title="任务详情">
        <Descriptions bordered column={2} size="small">
          <Descriptions.Item label="ID">{task.id}</Descriptions.Item>
          <Descriptions.Item label="状态"><Tag color={s.color}>{s.text}</Tag></Descriptions.Item>
          <Descriptions.Item label="任务名称">{task.name}</Descriptions.Item>
          <Descriptions.Item label="模板ID">{task.scenario_template_id}</Descriptions.Item>
          <Descriptions.Item label="快照ID">{task.template_snapshot_id}</Descriptions.Item>
          <Descriptions.Item label="日限量">{task.daily_limit}</Descriptions.Item>
          <Descriptions.Item label="最大并发">{task.max_concurrent}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{dayjs(task.created_at).format('YYYY-MM-DD HH:mm:ss')}</Descriptions.Item>
        </Descriptions>

        {task.schedule_config && (
          <div style={{ marginTop: 16 }}>
            <h4>调度配置</h4>
            <pre style={{ background: '#f5f5f5', padding: 12, borderRadius: 4 }}>
              {JSON.stringify(task.schedule_config, null, 2)}
            </pre>
          </div>
        )}

        {task.contact_filter && (
          <div style={{ marginTop: 16 }}>
            <h4>联系人筛选</h4>
            <pre style={{ background: '#f5f5f5', padding: 12, borderRadius: 4 }}>
              {JSON.stringify(task.contact_filter, null, 2)}
            </pre>
          </div>
        )}
      </Card>
    </div>
  );
}
