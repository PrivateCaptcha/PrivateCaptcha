UPDATE backend.properties
SET level = CASE
WHEN level = '{{ .OldSmallLevel }}'::smallint + (-1) * '{{ .OldDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + (-1) * '{{ .NewDelta }}'::smallint
WHEN level = '{{ .OldSmallLevel }}'::smallint + ( 0) * '{{ .OldDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + ( 0) * '{{ .NewDelta }}'::smallint
WHEN level = '{{ .OldSmallLevel }}'::smallint + ( 1) * '{{ .OldDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + ( 1) * '{{ .NewDelta }}'::smallint
WHEN level = '{{ .OldSmallLevel }}'::smallint + ( 2) * '{{ .OldDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + ( 2) * '{{ .NewDelta }}'::smallint
WHEN level = '{{ .OldSmallLevel }}'::smallint + ( 3) * '{{ .OldDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + ( 3) * '{{ .NewDelta }}'::smallint
ELSE GREATEST(LEAST(level + '{{ sub .NewSmallLevel .OldSmallLevel }}'::smallint, 255), 0)
END;
