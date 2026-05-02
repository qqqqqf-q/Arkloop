-- +goose Up
DELETE FROM personas WHERE persona_key = 'stem-tutor';

-- +goose Down
-- stem-tutor was removed as a persona; learning guidance is pipeline state.
