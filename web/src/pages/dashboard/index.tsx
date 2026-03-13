import { useEffect, useState } from 'react';
import { Card, Col, Row, Spin, Statistic, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PhoneOutlined, TeamOutlined, RocketOutlined, FileTextOutlined } from '@ant-design/icons';
import { listCalls } from '@/api/calls';
import { listContacts } from '@/api/contacts';
import { listTasks } from '@/api/tasks';
import { listTemplates } from '@/api/templates';
import type { CallListItem } from '@/types/api';

interface DashboardStats {
  totalCalls: number;
  publishedTemplates: number;
  totalContacts: number;
  activeTasks: number;
}

const statusColorMap: Record<string, string> = {
  queued: 'default',
  ringing: 'processing',
  answered: 'blue',
  in_progress: 'processing',
  completed: 'green',
  failed: 'red',
  no_answer: 'orange',
  busy: 'orange',
  cancelled: 'default',
};

const gradeColorMap: Record<string, string> = {
  A: 'green',
  B: 'blue',
  C: 'orange',
  D: 'red',
  E: 'default',
  F: 'default',
};

const recentCallColumns: ColumnsType<CallListItem> = [
  {
    title: 'ID',
    dataIndex: 'id',
    key: 'id',
    width: 80,
  },
  {
    title: '状态',
    dataIndex: 'status',
    key: 'status',
    width: 120,
    render: (status: string) => (
      <Tag color={statusColorMap[status] || 'default'}>{status}</Tag>
    ),
  },
  {
    title: '结果等级',
    dataIndex: 'result_grade',
    key: 'result_grade',
    width: 100,
    render: (grade: string | null) =>
      grade ? <Tag color={gradeColorMap[grade] || 'default'}>{grade}</Tag> : '-',
  },
  {
    title: '通话时长(秒)',
    dataIndex: 'duration',
    key: 'duration',
    width: 120,
    render: (duration: number | null) => (duration != null ? duration : '-'),
  },
  {
    title: '创建时间',
    dataIndex: 'created_at',
    key: 'created_at',
    render: (val: string) => new Date(val).toLocaleString('zh-CN'),
  },
];

export default function Dashboard() {
  const [loading, setLoading] = useState(true);
  const [stats, setStats] = useState<DashboardStats>({
    totalCalls: 0,
    publishedTemplates: 0,
    totalContacts: 0,
    activeTasks: 0,
  });
  const [recentCalls, setRecentCalls] = useState<CallListItem[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function fetchStats() {
      try {
        const [callsRes, contactsRes, tasksRes, templatesRes, recentRes] =
          await Promise.all([
            listCalls({ page: 1, page_size: 1 }),
            listContacts({ page: 1, page_size: 1 }),
            listTasks({ page: 1, page_size: 1, status: 'running' }),
            listTemplates({ page: 1, page_size: 1, status: 'active' }),
            listCalls({ page: 1, page_size: 5 }),
          ]);

        if (cancelled) return;

        setStats({
          totalCalls: callsRes.total,
          totalContacts: contactsRes.total,
          activeTasks: tasksRes.total,
          publishedTemplates: templatesRes.total,
        });
        setRecentCalls(recentRes.items);
      } catch {
        // errors are handled by the axios interceptor
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    fetchStats();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <Spin spinning={loading}>
      <div>
        <Row gutter={[16, 16]}>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic
                title="总通话数"
                value={stats.totalCalls}
                prefix={<PhoneOutlined />}
              />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic
                title="已发布模板"
                value={stats.publishedTemplates}
                prefix={<FileTextOutlined />}
              />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic
                title="联系人总数"
                value={stats.totalContacts}
                prefix={<TeamOutlined />}
              />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic
                title="活跃任务"
                value={stats.activeTasks}
                prefix={<RocketOutlined />}
              />
            </Card>
          </Col>
        </Row>

        <Card style={{ marginTop: 16 }} title="最近通话">
          <Table<CallListItem>
            columns={recentCallColumns}
            dataSource={recentCalls}
            rowKey="id"
            pagination={false}
            size="small"
            locale={{ emptyText: '暂无通话记录' }}
          />
        </Card>

        <Card style={{ marginTop: 16 }} title="系统概览">
          <p style={{ color: '#999' }}>
            系统就绪。后端 API 服务已连接，等待 Call Worker 和 FreeSWITCH 上线后即可开始外呼。
          </p>
        </Card>
      </div>
    </Spin>
  );
}
