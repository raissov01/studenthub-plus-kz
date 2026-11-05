package cafe

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
)

// showDemo: a demo café (is_demo) is listed only where CAFE_SHOW_DEMO=true (local and e2e), never by accident
// in production.
func showDemo() bool { return config.Bool("CAFE_SHOW_DEMO", false) }

const demoSizes = `[{"id":"size","nameKk":"Көлемі","nameRu":"Объём","required":true,"multi":false,"choices":[
 {"id":"s","nameKk":"250 мл","nameRu":"250 мл","delta":0},{"id":"m","nameKk":"350 мл","nameRu":"350 мл","delta":20000},{"id":"l","nameKk":"450 мл","nameRu":"450 мл","delta":40000}]},
 {"id":"milk","nameKk":"Сүт","nameRu":"Молоко","required":false,"multi":false,"choices":[
 {"id":"oat","nameKk":"Сұлы сүті","nameRu":"Овсяное","delta":30000},{"id":"lactose_free","nameKk":"Лактозасыз","nameRu":"Безлактозное","delta":20000}]},
 {"id":"syrup","nameKk":"Сироп","nameRu":"Сироп","required":false,"multi":true,"choices":[
 {"id":"vanilla","nameKk":"Ваниль","nameRu":"Ваниль","delta":15000},{"id":"caramel","nameKk":"Карамель","nameRu":"Карамель","delta":15000}]}]`

// SeedDemo adds «Demo Coffee» (is_demo, pay at pickup, open every day 08:00–20:00) with a small menu, once.
// Only for local development and the e2e database: cmd/cafe-seed refuses without CAFE_DEMO_SEED=dev.
func SeedDemo(ctx context.Context) (int64, error) {
	var id int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select id from cafes where is_demo and name_ru = 'Demo Coffee'`).Scan(&id)
		if err == nil || !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(ctx, `insert into cafes (name_kk, name_ru, building, place_kk, place_ru, open_time, close_time, open_days,
				slot_minutes, slot_capacity, lead_minutes, active, payment_mode, is_demo)
			values ('Demo Coffee', 'Demo Coffee', 'Бас ғимарат', '1-қабат, фойе (үлгі)', '1 этаж, фойе (пример)', '08:00', '20:00', '{1,2,3,4,5,6,7}',
				10, 6, 10, true, 'pickup', true) returning id`).Scan(&id); err != nil {
			return err
		}
		var coffee, food int64
		if err := tx.QueryRow(ctx, `insert into cafe_menu_categories (cafe_id, name_kk, name_ru, sort) values ($1, 'Кофе', 'Кофе', 1) returning id`, id).Scan(&coffee); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `insert into cafe_menu_categories (cafe_id, name_kk, name_ru, sort) values ($1, 'Тағамдар', 'Еда', 2) returning id`, id).Scan(&food); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into cafe_menu_items (cafe_id, category_id, name_kk, name_ru, desc_kk, desc_ru, price, options, sort) values
			($1, $2, 'Американо', 'Американо', 'Эспрессо мен ыстық су', 'Эспрессо и горячая вода', 70000, $4::jsonb, 1),
			($1, $2, 'Капучино', 'Капучино', 'Эспрессо, сүт көбігі', 'Эспрессо, молочная пенка', 90000, $4::jsonb, 2),
			($1, $2, 'Латте', 'Латте', 'Сүті көп, жұмсақ', 'Больше молока, мягкий вкус', 100000, $4::jsonb, 3),
			($1, $3, 'Круассан', 'Круассан', 'Сары майлы', 'Сливочный', 60000, '[]', 1),
			($1, $3, 'Сэндвич тауықпен', 'Сэндвич с курицей', '', '', 120000, '[]', 2)`, id, coffee, food, demoSizes)
		return err
	})
	return id, err
}
