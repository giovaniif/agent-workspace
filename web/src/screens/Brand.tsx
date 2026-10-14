export function Brand({ host }: { host: string }) {
  return (
    <div className="brand">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcSet="/crew-wordmark-mocha.svg" />
        <img src="/crew-wordmark.svg" alt="crew" />
      </picture>
      <span className="host">{host}</span>
    </div>
  );
}
