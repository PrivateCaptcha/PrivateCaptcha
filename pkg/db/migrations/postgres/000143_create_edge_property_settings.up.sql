CREATE TYPE backend.edge_widget_start_mode AS ENUM ('click', 'load');

ALTER TABLE backend.properties ADD CONSTRAINT properties_id_external_id_key UNIQUE (id, external_id);

CREATE TABLE backend.edge_property_settings (
    property_id INT PRIMARY KEY,
    external_id UUID NOT NULL,
    edge_widget_start_mode backend.edge_widget_start_mode NOT NULL DEFAULT 'click',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    FOREIGN KEY (property_id, external_id) REFERENCES backend.properties(id, external_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX index_edge_property_settings_external_id ON backend.edge_property_settings(external_id);
