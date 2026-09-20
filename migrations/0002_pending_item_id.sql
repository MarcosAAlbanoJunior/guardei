-- A espera por descrição de um item já salvo (atualizar/editar) ganha coluna
-- própria, em vez de ficar codificada como "atualizar:<id>" em reason.
ALTER TABLE pending ADD COLUMN item_id INTEGER;

UPDATE pending
   SET item_id = CAST(substr(reason, 11) AS INTEGER), reason = ''
 WHERE reason LIKE 'atualizar:%';
