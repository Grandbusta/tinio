import { useEffect, useState, type ReactNode } from "react";
import { BrowserRouter, Navigate, Outlet, Route, Routes, useParams } from "react-router";
import { SettingsPage } from "@/components/agent-settings";
import { AllAgents } from "@/components/all-agents";
import { AppFrame } from "@/components/app-frame";
import { CharacterPage } from "@/components/character";
import { Chat } from "@/components/chat";
import { AgentToolsPage, ConnectionsPage, ConnectPage } from "@/components/connections";
import { JobPage } from "@/components/job-page";
import { FirstRun, keySkipped, ready, skipKeyForNow, useSources } from "@/components/first-run";
import { AddFolderPage } from "@/components/folders";
import { GrowthPage } from "@/components/growth";
import { JudgmentPage } from "@/components/judgment";
import { MemoryPage } from "@/components/memory";
import { MindPage } from "@/components/mind";
import { PageError } from "@/components/page-error";
import { PickCharacter } from "@/components/pick-character";
import { SettingsPage as AppSettings } from "@/components/settings";
import { SkillImportPage, SkillsPage } from "@/components/skills";
import { TodayPage } from "@/components/today";
import { TopicsPage } from "@/components/topics";
import { YouPage } from "@/components/usage";
import { WhoYouAre } from "@/components/who-you-are";
import { WorkPage, WorksPage } from "@/components/works";

// The app's pages, at the web app's addresses. Before anything, where your agents think
// (Claude Code or your OpenAI key), the one thing you bring; it can wait, but nobody
// replies without it.
export function App() {
  const [sources, reload] = useSources();
  const [skipped, setSkipped] = useState(keySkipped);
  if (!sources) return <Starting />;
  if (!ready(sources) && !skipped)
    return (
      <FirstRun
        sources={sources}
        onDone={reload}
        onAgain={reload}
        onSkip={() => {
          skipKeyForNow();
          setSkipped(true);
        }}
      />
    );
  return (
    <BrowserRouter>
      <PageError>
      <Routes>
        <Route path="/" element={<Navigate to="/agents" replace />} />
        <Route path="/raise" element={<Titled title="Pick a character"><PickCharacter /></Titled>} />
        <Route path="/raise/:character" element={<Param name="character" render={(c) => <Titled title="Who are you to them"><WhoYouAre characterKey={c} /></Titled>} />} />
        <Route element={<AppFrame><PageError><Outlet /></PageError></AppFrame>}>
          <Route path="/agents" element={<Titled title="Your agents"><AllAgents /></Titled>} />
          <Route path="/you" element={<Titled title="You"><YouPage /></Titled>} />
          <Route path="/settings" element={<Titled title="Settings"><AppSettings /></Titled>} />
          <Route path="/settings/folders/add" element={<Titled title="Add a folder"><AddFolderPage /></Titled>} />
          <Route path="/today" element={<Titled title="Your day"><TodayPage /></Titled>} />
          <Route path="/connections" element={<Connections />} />
          <Route path="/connections/:connector" element={<Connect />} />
          <Route path="/agents/:id" element={<Agent title="Chat" page={(id) => <Chat id={id} />} />} />
          <Route path="/agents/:id/today" element={<Agent title="Your day" page={(id) => <TodayPage id={id} />} />} />
          <Route path="/agents/:id/works" element={<Agent title="Works" page={(id) => <WorksPage id={id} />} />} />
          <Route path="/agents/:id/works/:work" element={<Work />} />
          <Route path="/agents/:id/jobs/:job" element={<JobRoute />} />
          <Route path="/agents/:id/skills" element={<Agent title="Skills" page={(id) => <SkillsPage id={id} />} />} />
          <Route path="/agents/:id/skills/import" element={<Agent title="Import a skill" page={(id) => <SkillImportPage id={id} />} />} />
          <Route path="/agents/:id/topics" element={<Agent title="Topics" page={(id) => <TopicsPage id={id} />} />} />
          <Route path="/agents/:id/character" element={<Agent title="Character" page={(id) => <CharacterPage id={id} />} />} />
          <Route path="/agents/:id/judgment" element={<Agent title="How you decide" page={(id) => <JudgmentPage id={id} />} />} />
          <Route path="/agents/:id/memory" element={<Agent title="Memory" page={(id) => <MemoryPage id={id} />} />} />
          <Route path="/agents/:id/mind" element={<Agent title="Mind" page={(id) => <MindPage id={id} />} />} />
          <Route path="/agents/:id/growth" element={<Agent title="Helping you" page={(id) => <GrowthPage id={id} />} />} />
          <Route path="/agents/:id/tools" element={<Agent title="Tools" page={(id) => <AgentToolsPage id={id} />} />} />
          <Route path="/agents/:id/settings" element={<Agent title="Settings" page={(id) => <SettingsPage id={id} />} />} />
        </Route>
        <Route path="*" element={<Navigate to="/agents" replace />} />
      </Routes>
      </PageError>
    </BrowserRouter>
  );
}

function Starting() {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setSlow(true), 3000);
    return () => clearTimeout(t);
  }, []);
  return <div className="grid min-h-dvh place-items-center bg-bg text-sm text-muted">{slow ? "Tinio is starting…" : ""}</div>;
}

function Titled({ title, children }: { title: string; children: ReactNode }) {
  useEffect(() => {
    document.title = `${title} · Tinio`;
  }, [title]);
  return <>{children}</>;
}

function Param({ name, render }: { name: string; render: (v: string) => ReactNode }) {
  const v = useParams()[name] ?? "";
  return <>{render(v)}</>;
}

// An agent's page: keyed by the agent, so moving between agents starts it afresh.
function Agent({ title, page }: { title: string; page: (id: string) => ReactNode }) {
  const id = useParams().id ?? "";
  return (
    <Titled title={title}>
      <PageFor key={id}>{page(id)}</PageFor>
    </Titled>
  );
}

function PageFor({ children }: { children: ReactNode }) {
  return <>{children}</>;
}

function JobRoute() {
  const { id = "", job = "" } = useParams();
  return (
    <Titled title="Coding job">
      <JobPage key={`${id}/${job}`} agent={id} job={job} />
    </Titled>
  );
}

function Work() {
  const { id = "", work = "" } = useParams();
  return (
    <Titled title="Work">
      <WorkPage key={`${id}/${work}`} id={id} work={work} />
    </Titled>
  );
}

function Connections() {
  const failed = new URLSearchParams(window.location.search).get("failed") ?? undefined;
  return (
    <Titled title="Connections">
      <ConnectionsPage failed={failed} />
    </Titled>
  );
}

function Connect() {
  const { connector = "" } = useParams();
  const q = new URLSearchParams(window.location.search);
  return (
    <Titled title="Connect">
      <ConnectPage connector={connector} connected={q.get("connected") ?? undefined} forId={q.get("for") ?? undefined} failed={q.get("failed") ?? undefined} />
    </Titled>
  );
}
