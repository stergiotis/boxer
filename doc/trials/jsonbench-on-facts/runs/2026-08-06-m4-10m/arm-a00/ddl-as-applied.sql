CREATE TABLE bluesky
(
    `data` JSON CODEC(ZSTD(1))
)
ORDER BY tuple()

-- Below settings are planned to be default soon
SETTINGS object_serialization_version = 'v3',
         dynamic_serialization_version = 'v3',
         object_shared_data_serialization_version = 'advanced',
         object_shared_data_serialization_version_for_zero_level_parts='map_with_buckets'
