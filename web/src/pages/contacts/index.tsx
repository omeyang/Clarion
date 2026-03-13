import { useRef, useState } from 'react';
import { Button, Tag, Space, message, Modal, Input, Switch } from 'antd';
import { PlusOutlined, UploadOutlined } from '@ant-design/icons';
import { ProTable, type ActionType, type ProColumns } from '@ant-design/pro-components';
import { listContacts, createContact, batchCreateContacts } from '@/api/contacts';
import type { Contact } from '@/types/api';

const statusMap: Record<string, { color: string; text: string }> = {
  new: { color: 'blue', text: '新建' },
  called: { color: 'green', text: '已拨打' },
  no_answer: { color: 'orange', text: '未接' },
  busy: { color: 'red', text: '占线' },
  blacklisted: { color: 'default', text: '黑名单' },
};

export default function ContactList() {
  const actionRef = useRef<ActionType>();
  const [createOpen, setCreateOpen] = useState(false);
  const [batchOpen, setBatchOpen] = useState(false);

  const columns: ProColumns<Contact>[] = [
    { title: 'ID', dataIndex: 'id', width: 60, search: false },
    { title: '手机号(脱敏)', dataIndex: 'phone_masked', width: 140, copyable: true },
    { title: '来源', dataIndex: 'source', width: 120 },
    {
      title: '状态',
      dataIndex: 'current_status',
      width: 90,
      valueEnum: Object.fromEntries(Object.entries(statusMap).map(([k, v]) => [k, { text: v.text }])),
      render: (_, r) => {
        const s = statusMap[r.current_status] || { color: 'default', text: r.current_status };
        return <Tag color={s.color}>{s.text}</Tag>;
      },
    },
    {
      title: '免打扰',
      dataIndex: 'do_not_call',
      width: 80,
      search: false,
      render: (_, r) => r.do_not_call ? <Tag color="red">是</Tag> : <Tag color="green">否</Tag>,
    },
    { title: '创建时间', dataIndex: 'created_at', valueType: 'dateTime', width: 170, search: false },
    {
      title: '扩展信息',
      dataIndex: 'profile_json',
      search: false,
      ellipsis: true,
      render: (_, r) => r.profile_json ? JSON.stringify(r.profile_json) : '-',
    },
  ];

  return (
    <>
      <ProTable<Contact>
        actionRef={actionRef}
        columns={columns}
        rowKey="id"
        headerTitle="联系人管理"
        request={async (params) => {
          const res = await listContacts({
            page: params.current,
            page_size: params.pageSize,
            source: params.source,
            status: params.current_status,
          });
          return { data: res.items, total: res.total, success: true };
        }}
        pagination={{ defaultPageSize: 20 }}
        toolBarRender={() => [
          <Button key="create" type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            添加联系人
          </Button>,
          <Button key="batch" icon={<UploadOutlined />} onClick={() => setBatchOpen(true)}>
            批量导入
          </Button>,
        ]}
      />
      <CreateContactModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onSuccess={() => { setCreateOpen(false); actionRef.current?.reload(); }}
      />
      <BatchImportModal
        open={batchOpen}
        onClose={() => setBatchOpen(false)}
        onSuccess={() => { setBatchOpen(false); actionRef.current?.reload(); }}
      />
    </>
  );
}

function CreateContactModal({
  open, onClose, onSuccess,
}: { open: boolean; onClose: () => void; onSuccess: () => void }) {
  const [phoneMasked, setPhoneMasked] = useState('');
  const [phoneHash, setPhoneHash] = useState('');
  const [source, setSource] = useState('');
  const [doNotCall, setDoNotCall] = useState(false);
  const [loading, setLoading] = useState(false);

  const handleOk = async () => {
    if (!phoneMasked.trim() || !phoneHash.trim()) {
      message.warning('请填写手机号');
      return;
    }
    setLoading(true);
    try {
      await createContact({
        phone_masked: phoneMasked.trim(),
        phone_hash: phoneHash.trim(),
        source: source.trim() || undefined,
        do_not_call: doNotCall,
      });
      message.success('添加成功');
      setPhoneMasked('');
      setPhoneHash('');
      setSource('');
      setDoNotCall(false);
      onSuccess();
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal title="添加联系人" open={open} onCancel={onClose} onOk={handleOk} confirmLoading={loading}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16, paddingTop: 8 }}>
        <div>
          <label>手机号(脱敏)</label>
          <Input value={phoneMasked} onChange={(e) => setPhoneMasked(e.target.value)} placeholder="138****1234" style={{ marginTop: 4 }} />
        </div>
        <div>
          <label>手机号 Hash</label>
          <Input value={phoneHash} onChange={(e) => setPhoneHash(e.target.value)} placeholder="SHA256 hash" style={{ marginTop: 4 }} />
        </div>
        <div>
          <label>来源</label>
          <Input value={source} onChange={(e) => setSource(e.target.value)} placeholder="例如：58同城" style={{ marginTop: 4 }} />
        </div>
        <div>
          <label style={{ marginRight: 8 }}>免打扰</label>
          <Switch checked={doNotCall} onChange={setDoNotCall} />
        </div>
      </div>
    </Modal>
  );
}

function BatchImportModal({
  open, onClose, onSuccess,
}: { open: boolean; onClose: () => void; onSuccess: () => void }) {
  const [text, setText] = useState('');
  const [source, setSource] = useState('');
  const [loading, setLoading] = useState(false);

  const handleOk = async () => {
    const lines = text.trim().split('\n').filter(Boolean);
    if (lines.length === 0) {
      message.warning('请输入联系人数据');
      return;
    }
    setLoading(true);
    try {
      const items = lines.map((line) => {
        const [masked, hash] = line.split(',').map((s) => s.trim());
        return { phone_masked: masked, phone_hash: hash || masked, source: source.trim() || undefined };
      });
      const result = await batchCreateContacts(items);
      message.success(`导入完成：创建 ${result.created}，跳过 ${result.skipped}`);
      setText('');
      setSource('');
      onSuccess();
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal title="批量导入联系人" open={open} onCancel={onClose} onOk={handleOk} confirmLoading={loading} width={600}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16, paddingTop: 8 }}>
        <div>
          <label>来源</label>
          <Input value={source} onChange={(e) => setSource(e.target.value)} placeholder="例如：3月批次" style={{ marginTop: 4 }} />
        </div>
        <div>
          <label>联系人数据（每行一条，格式：脱敏手机号,hash）</label>
          <Input.TextArea
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={10}
            placeholder={"138****1234,a1b2c3d4e5f6...\n139****5678,f6e5d4c3b2a1..."}
            style={{ marginTop: 4 }}
          />
        </div>
      </div>
    </Modal>
  );
}
