UPDATE OR IGNORE automation_trust SET key = replace(key, '.agentws.toml', '.crew.toml'), data = replace(data, '.agentws.toml', '.crew.toml') WHERE key LIKE '%.agentws.toml%';
