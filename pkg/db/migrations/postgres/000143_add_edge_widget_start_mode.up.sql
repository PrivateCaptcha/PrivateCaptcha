CREATE TYPE backend.edge_widget_start_mode AS ENUM ('click', 'load');

ALTER TABLE backend.properties ADD COLUMN edge_widget_start_mode backend.edge_widget_start_mode NOT NULL DEFAULT 'click';
