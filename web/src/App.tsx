import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from '@/layouts/MainLayout';
import Dashboard from '@/pages/dashboard';
import TemplateList from '@/pages/templates';
import TemplateDetail from '@/pages/templates/TemplateDetail';
import TaskList from '@/pages/tasks';
import TaskDetail from '@/pages/tasks/TaskDetail';
import ContactList from '@/pages/contacts';
import CallList from '@/pages/calls';
import CallDetail from '@/pages/calls/CallDetail';

export default function App() {
  return (
    <ConfigProvider locale={zhCN}>
      <BrowserRouter>
        <Routes>
          <Route element={<MainLayout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/templates" element={<TemplateList />} />
            <Route path="/templates/:id" element={<TemplateDetail />} />
            <Route path="/tasks" element={<TaskList />} />
            <Route path="/tasks/:id" element={<TaskDetail />} />
            <Route path="/contacts" element={<ContactList />} />
            <Route path="/calls" element={<CallList />} />
            <Route path="/calls/:id" element={<CallDetail />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ConfigProvider>
  );
}
