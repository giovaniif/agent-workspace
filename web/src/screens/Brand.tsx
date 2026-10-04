export function Brand({ host }: { host: string }) {
  return (
    <div className="brand">
      <img src="/icon.svg" alt="" />
      <span>agentws · {host}</span>
    </div>
  );
}
