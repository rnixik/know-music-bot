CREATE TABLE `sounds` (
	`id` INT(11) NOT NULL AUTO_INCREMENT,
	`title` VARCHAR(255) NOT NULL DEFAULT '',
	`file_id` VARCHAR(255) NOT NULL,
	`genre` VARCHAR(255) NOT NULL DEFAULT 'game',
	PRIMARY KEY (`id`)
)
COLLATE='utf8_general_ci'
ENGINE=InnoDB;
