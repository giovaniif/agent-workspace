package domain

const envPrefix = "CREW_"

func EnvName(key string) string { return envPrefix + key }

func Env(get func(string) string, key string) string {
	if v := get(envPrefix + key); v != "" {
		return v
	}
	return get(LegacyEnvPrefix + key)
}
