import { useRef, useState } from 'react';
import { Button, Tag, Space, Popconfirm, message, Modal, Input } from 'antd';
import { PlusOutlined, SendOutlined } from '@ant-design/icons';
import { ProTable, type ActionType, type ProColumns } from '@ant-design/pro-components';
import { useNavigate } from 'react-router-dom';
import { listTemplates, deleteTemplate, publishTemplate, createTemplate } from '@/api/templates';
import type { TemplateListItem } from '@/types/api';

const statusMap: Record<string, { color: string; text: string }> = {
  draft: { color: 'default', text: '草稿' },
  published: { color: 'green', text: '已发布' },
  archived: { color: 'orange', text: '已归档' },
};

export default function TemplateList() {
  const actionRef = useRef<ActionType>();
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);

  const columns: ProColumns<TemplateListItem>[] = [
    { title: 'ID', dataIndex: 'id', width: 60, search: false },
    { title: '模板名称', dataIndex: 'name', ellipsis: true },
    { title: '领域', dataIndex: 'domain', width: 100 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      valueEnum: { draft: { text: '草稿' }, published: { text: '已发布' }, archived: { text: '已归档' } },
      render: (_, record) => {
        const s = statusMap[record.status] || { color: 'default', text: record.status };
        return <Tag color={s.color}>{s.text}</Tag>;
      },
    },
    { title: '版本', dataIndex: 'version', width: 60, search: false },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      valueType: 'dateTime',
      width: 170,
      search: false,
    },
    {
      title: '操作',
      width: 200,
      search: false,
      render: (_, record) => (
        <Space>
          <a onClick={() => navigate(`/templates/${record.id}`)}>编辑</a>
          {record.status === 'draft' && (
            <Popconfirm
              title="确定发布此模板？"
              onConfirm={async () => {
                await publishTemplate(record.id);
                message.success('发布成功');
                actionRef.current?.reload();
              }}
            >
              <a><SendOutlined /> 发布</a>
            </Popconfirm>
          )}
          <Popconfirm
            title="确定删除此模板？"
            onConfirm={async () => {
              await deleteTemplate(record.id);
              message.success('已删除');
              actionRef.current?.reload();
            }}
          >
            <a style={{ color: '#ff4d4f' }}>删除</a>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <>
      <ProTable<TemplateListItem>
        actionRef={actionRef}
        columns={columns}
        rowKey="id"
        headerTitle="话术模板"
        request={async (params) => {
          const res = await listTemplates({
            page: params.current,
            page_size: params.pageSize,
            status: params.status,
            domain: params.domain,
          });
          return { data: res.items, total: res.total, success: true };
        }}
        pagination={{ defaultPageSize: 20 }}
        toolBarRender={() => [
          <Button
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => setCreateOpen(true)}
          >
            新建模板
          </Button>,
        ]}
      />
      <CreateTemplateModal
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

function CreateTemplateModal({
  open,
  onClose,
  onSuccess,
}: {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [name, setName] = useState('');
  const [domain, setDomain] = useState('');
  const [loading, setLoading] = useState(false);

  const handleOk = async () => {
    if (!name.trim() || !domain.trim()) {
      message.warning('请填写模板名称和领域');
      return;
    }
    setLoading(true);
    try {
      await createTemplate({ name: name.trim(), domain: domain.trim() });
      message.success('创建成功');
      setName('');
      setDomain('');
      onSuccess();
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal title="新建模板" open={open} onCancel={onClose} onOk={handleOk} confirmLoading={loading}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16, paddingTop: 8 }}>
        <div>
          <label>模板名称</label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="例如：房产初筛话术"
            style={{ marginTop: 4 }}
          />
        </div>
        <div>
          <label>领域</label>
          <Input
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="例如：real_estate"
            style={{ marginTop: 4 }}
          />
        </div>
      </div>
    </Modal>
  );
}
